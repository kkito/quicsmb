#ifndef QUICSMB_QUIC_H
#define QUICSMB_QUIC_H

// QUIC transport abstraction. With MsQuic present this wraps real QUIC
// connections/streams; without it a stub backend (plain TCP passthrough,
// framing-compatible) is used so the project still builds and the tunnel
// logic + tests run.
#include "quicsmb/common.h"
#include <stddef.h>
#include <stdint.h>

typedef struct qs_quic qs_quic_t;     // process-wide QUIC endpoint
typedef struct qs_conn qs_conn_t;     // one QUIC connection
typedef struct qs_stream qs_stream_t; // one bidi stream

typedef void (*qs_stream_data_cb)(qs_stream_t *s, const uint8_t *data, size_t len, void *ctx);
typedef void (*qs_stream_close_cb)(qs_stream_t *s, void *ctx);

typedef struct {
    qs_stream_data_cb on_data;
    qs_stream_close_cb on_close;
    void *ctx;
} qs_stream_cbs_t;

// --- endpoint lifecycle ---
int qs_quic_init_client(qs_quic_t **out, const char *alpn);
int qs_quic_init_client_insecure(qs_quic_t **out, const char *alpn, int insecure);
// PoC always skips server cert validation (self-signed cert.pem); call is
// a no-op for the stub backend.
void qs_quic_set_insecure(qs_quic_t *q, int insecure);
int qs_quic_init_server(qs_quic_t **out, const char *alpn,
                        const char *cert_file, const char *key_file, int port);
void qs_quic_free(qs_quic_t *q);
int qs_quic_has_real_quic(void); // 1 if MsQuic backend, 0 if stub

// --- client side ---
qs_conn_t *qs_dial(qs_quic_t *q, const char *host, int port);

// --- server side: server calls accept_cb for each inbound stream ---
typedef void (*qs_accept_cb)(qs_quic_t *q, qs_stream_t *s, void *ctx);
void qs_server_set_accept(qs_quic_t *q, qs_accept_cb cb, void *ctx);

// --- streams ---
// client: open a new bidi stream on conn. server: streams arrive via accept_cb.
qs_stream_t *qs_stream_open(qs_conn_t *conn, qs_stream_cbs_t cbs);
void qs_stream_set_cbs(qs_stream_t *s, qs_stream_cbs_t cbs);
// non-blocking enqueue; returns 0 ok, <0 closed/error
int qs_stream_send(qs_stream_t *s, const uint8_t *data, size_t len);
// half-close our send direction (SMB needs full-duplex; only used at teardown)
void qs_stream_shutdown_write(qs_stream_t *s);
void qs_stream_close(qs_stream_t *s);

#endif
