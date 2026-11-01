#ifndef QUICSMB_COMMON_H
#define QUICSMB_COMMON_H

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>

#define QUICSMB_ALPN "quicsmb/1"
// Microsoft native interop ALPN (server must present ALPN "smb" for Windows clients)
#define QUICSMB_ALPN_SMB "smb"

// Default ports
#define QUICSMB_QUIC_PORT 4433   // our tunnel default (avoid needing root for 443 in dev)
#define QUICSMB_SMB_PORT 445
#define QUICSMB_TCP_LISTEN_PORT 8445

// One TCP connection <-> one QUIC bidi stream. Streams are ordered byte
// streams like TCP, so payload is transparent (no extra framing).
#define QUICSMB_STREAM_BUF 65536

typedef enum { QS_CLIENT = 0, QS_SERVER = 1 } qs_mode_t;

typedef enum {
    QS_PROTO_SMB2 = 0xFE534D42u // 'SMB' + 0xFE little-endian ProtocolId
} qs_proto_t;

#endif
