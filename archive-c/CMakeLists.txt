cmake_minimum_required(VERSION 3.20)
project(quicsmb C)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_C_EXTENSIONS OFF)

option(QUICSMB_BUILD_TESTS "Build tests" ON)
option(QUICSMB_USE_VCPKG "Use vcpkg toolchain" ON)

# System-toolchain builds use strict -std=c11, under which glibc hides
# struct addrinfo / CLOCK_REALTIME. Feature-test macros fix this globally.
if(NOT APPLE AND NOT WIN32)
  add_compile_definitions(_POSIX_C_SOURCE=200809L _DEFAULT_SOURCE)
endif()

if(QUICSMB_USE_VCPKG)
  find_package(Libevent CONFIG REQUIRED)
  find_package(cJSON CONFIG REQUIRED)
else()
  find_package(PkgConfig REQUIRED)
  pkg_check_modules(LIBEVENT REQUIRED libevent libevent_pthreads)
  pkg_check_modules(CJSON libcjson)
  if(NOT CJSON_FOUND)
    # Debian/Ubuntu name it libcjson, RHEL/Fedora ship cjson.pc
    pkg_check_modules(CJSON REQUIRED cjson)
  endif()
  add_library(libevent_iface INTERFACE)
  target_include_directories(libevent_iface INTERFACE ${LIBEVENT_INCLUDE_DIRS})
  target_link_libraries(libevent_iface INTERFACE ${LIBEVENT_LIBRARIES})
  add_library(cjson_iface INTERFACE)
  target_include_directories(cjson_iface INTERFACE ${CJSON_INCLUDE_DIRS})
  target_link_libraries(cjson_iface INTERFACE ${CJSON_LIBRARIES})
endif()
# MsQuic is optional at configure time: if absent we build a stub QUIC
# backend (TCP passthrough) so `make` works before `vcpkg install msquic`.
#
# The vcpkg msquic port is dynamic-only (vcpkg_check_linkage ONLY_DYNAMIC)
# and its static build does not export a CMake package (no install(EXPORT)
# in the static branch). We therefore detect the static archive directly:
#   1. find_package(msquic)                -> dynamic libmsquic (preferred)
#   2. libmsquic.a in the vcpkg install dir -> static quictls build
#   3. system msquic.h + libmsquic          -> fallback
#   4. none                                 -> stub backend
set(QUICSMB_HAVE_MSQUIC 0)
# P0: vendored monolithic static lib (third_party/msquic) — contains core +
# platform + quictls OpenSSL, no external crypto dependency, no symbol clash.
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/third_party/msquic/lib/libmsquic.a"
   AND EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/third_party/msquic/include/msquic.h")
  set(QUICSMB_HAVE_MSQUIC 1)
  add_library(msquic_static UNKNOWN IMPORTED)
  set_target_properties(msquic_static PROPERTIES
    IMPORTED_LOCATION "${CMAKE_CURRENT_SOURCE_DIR}/third_party/msquic/lib/libmsquic.a"
    INTERFACE_INCLUDE_DIRECTORIES "${CMAKE_CURRENT_SOURCE_DIR}/third_party/msquic/include"
    INTERFACE_COMPILE_DEFINITIONS "QUIC_BUILD_STATIC")
  if(APPLE)
    set_property(TARGET msquic_static APPEND PROPERTY
      INTERFACE_LINK_LIBRARIES "-framework CoreFoundation" "-framework Security")
  endif()
  set(QUICSMB_MSQUIC_TARGET msquic_static)
  message(STATUS "msquic: using vendored static libmsquic.a (quictls inside)")
endif()
if(NOT QUICSMB_HAVE_MSQUIC)
  find_package(msquic CONFIG QUIET)
  if(msquic_FOUND)
    set(QUICSMB_HAVE_MSQUIC 1)
    if(TARGET msquic::msquic)
      set(QUICSMB_MSQUIC_TARGET msquic::msquic)
    else()
      set(QUICSMB_MSQUIC_TARGET msquic)
    endif()
    message(STATUS "msquic: using dynamic package target ${QUICSMB_MSQUIC_TARGET}")
  endif()
endif()
if(NOT QUICSMB_HAVE_MSQUIC)
  # Static monolithic archive produced by -DQUIC_BUILD_SHARED=OFF builds
  # (contains core + platform + vendored quictls OpenSSL).
  find_file(MSQUIC_STATIC_LIB NAMES libmsquic.a
    PATHS "${VCPKG_INSTALLED_DIR}/${VCPKG_TARGET_TRIPLET}/lib"
          "${VCPKG_INSTALLED_DIR}/${VCPKG_TARGET_TRIPLET}/debug/lib"
    NO_DEFAULT_PATH)
  find_path(MSQUIC_STATIC_INC NAMES msquic.h
    PATHS "${VCPKG_INSTALLED_DIR}/${VCPKG_TARGET_TRIPLET}/include"
    NO_DEFAULT_PATH)
  if(MSQUIC_STATIC_LIB AND MSQUIC_STATIC_INC)
    set(QUICSMB_HAVE_MSQUIC 1)
    add_library(msquic_static UNKNOWN IMPORTED)
    set_target_properties(msquic_static PROPERTIES
      IMPORTED_LOCATION "${MSQUIC_STATIC_LIB}"
      INTERFACE_INCLUDE_DIRECTORIES "${MSQUIC_STATIC_INC}"
      INTERFACE_COMPILE_DEFINITIONS "QUIC_BUILD_STATIC")
    if(APPLE)
      set_property(TARGET msquic_static APPEND PROPERTY
        INTERFACE_LINK_LIBRARIES "-framework CoreFoundation" "-framework Security")
    endif()
    set(QUICSMB_MSQUIC_TARGET msquic_static)
    message(STATUS "msquic: using static archive ${MSQUIC_STATIC_LIB}")
  else()
    find_path(MSQUIC_INCLUDE_DIR NAMES msquic.h)
    find_library(MSQUIC_LIB NAMES msquic)
    if(MSQUIC_INCLUDE_DIR AND MSQUIC_LIB)
      set(QUICSMB_HAVE_MSQUIC 1)
      add_library(msquic_fallback UNKNOWN IMPORTED)
      set_target_properties(msquic_fallback PROPERTIES
        IMPORTED_LOCATION "${MSQUIC_LIB}"
        INTERFACE_INCLUDE_DIRECTORIES "${MSQUIC_INCLUDE_DIR}")
      set(QUICSMB_MSQUIC_TARGET msquic_fallback)
      message(STATUS "msquic: using system library ${MSQUIC_LIB}")
    else()
      message(STATUS "msquic not found: building with stub QUIC backend (TCP passthrough). Install msquic via vcpkg for real QUIC transport.")
    endif()
  endif()
endif()

file(GLOB_RECURSE QUICSMB_SRCS CONFIGURE_DEPENDS "${CMAKE_CURRENT_SOURCE_DIR}/src/*.c")
set(QUICSMB_MAIN "")
foreach(f ${QUICSMB_SRCS})
  if(f MATCHES ".*/main\\.c$")
    set(QUICSMB_MAIN ${f})
    list(REMOVE_ITEM QUICSMB_SRCS ${f})
  endif()
endforeach()

add_library(quicsmb_core STATIC ${QUICSMB_SRCS})
target_include_directories(quicsmb_core PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)
if(QUICSMB_HAVE_MSQUIC)
  target_compile_definitions(quicsmb_core PUBLIC QUICSMB_HAVE_MSQUIC=1)
  target_link_libraries(quicsmb_core PUBLIC ${QUICSMB_MSQUIC_TARGET})
else()
  target_compile_definitions(quicsmb_core PUBLIC QUICSMB_HAVE_MSQUIC=0)
endif()
if(QUICSMB_USE_VCPKG)
  target_link_libraries(quicsmb_core PUBLIC
    libevent::core libevent::extra libevent::pthreads
    cjson)
else()
  target_include_directories(quicsmb_core PUBLIC ${LIBEVENT_INCLUDE_DIRS} ${CJSON_INCLUDE_DIRS})
  target_link_libraries(quicsmb_core PUBLIC ${LIBEVENT_LIBRARIES} ${CJSON_LIBRARIES})
endif()
if(NOT QUICSMB_HAVE_MSQUIC)
  # stub backend only
  find_package(OpenSSL REQUIRED)
  target_link_libraries(quicsmb_core PUBLIC OpenSSL::SSL OpenSSL::Crypto)
endif()

add_executable(quicsmb ${QUICSMB_MAIN})
target_link_libraries(quicsmb PRIVATE quicsmb_core)

if(QUICSMB_BUILD_TESTS)
  enable_testing()
  add_subdirectory(tests)
endif()
