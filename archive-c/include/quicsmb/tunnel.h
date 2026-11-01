#ifndef QUICSMB_TUNNEL_H
#define QUICSMB_TUNNEL_H

// Tunnel: TCP <-> QUIC stream relay. Mirrors rybaproxy relay.c design:
// one session = one TCP bufferevent + one QUIC stream, pump both ways,
// count bytes, sniff first SMB2 header for logging.
#include <event2/event.h>
#include <event2/bufferevent.h>

typedef struct qs_session qs_session_t;
struct qs_session {
    uint64_t id;
    struct bufferevent *bev_tcp; // owned
    struct qs_stream *stream;    // owned
    uint64_t bytes_up;           // tcp -> quic
    uint64_t bytes_down;         // quic -> tcp
    int logged_handshake;
    volatile int dead;           // set on first teardown (either side)
};

typedef void (*qs_session_done_cb)(qs_session_t *s, void *ctx);

// Attach an already-connected TCP bev and QUIC stream; tunnel starts pumping.
qs_session_t *qs_tunnel_start(struct event_base *base,
                              struct bufferevent *bev_tcp,
                              struct qs_stream *stream,
                              qs_session_done_cb done_cb, void *ctx);
void qs_tunnel_free(qs_session_t *s);

#endif
