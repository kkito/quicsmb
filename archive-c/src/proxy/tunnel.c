// TCP <-> QUIC stream relay. Mirrors rybaproxy/proxy/relay.c pump design:
// bufferevent input -> qs_stream_send, stream data -> bev output.
#include "quicsmb/tunnel.h"
#include "quicsmb/quic.h"
#include "quicsmb/smb.h"
#include "quicsmb/log.h"

#include <event2/buffer.h>
#include <stdlib.h>
#include <string.h>

struct qs_tunnel_ctx {
    struct event_base *base;
    qs_session_done_cb done_cb;
    void *done_ctx;
    int done;
};

// Teardown exactly once (guarded by s->dead): called from TCP event, stream
// close, or owner. Frees bev + stream; caller frees session after.
static void tunnel_teardown(qs_session_t *s) {
    if (!s || __sync_lock_test_and_set(&s->dead, 1)) return;
    if (s->bev_tcp) {
        bufferevent_free(s->bev_tcp);
        s->bev_tcp = NULL;
    }
    if (s->stream) {
        qs_stream_t *st = s->stream;
        s->stream = NULL;
        // If we are inside the reader thread's on_close, closing from here
        // would join self; the stub handles that case via detach.
        qs_stream_close(st);
    }
}

static void on_tcp_read(struct bufferevent *bev, void *ctx) {
    qs_session_t *s = ctx;
    struct evbuffer *in = bufferevent_get_input(bev);
    size_t len = evbuffer_get_length(in);
    if (len == 0 || !s->stream) return;
    // peek for SMB2 handshake logging
    if (!s->logged_handshake && len >= 68) {
        uint8_t peek[72];
        size_t n = evbuffer_copyout(in, peek, sizeof peek);
        smb2_info_t info;
        if (n >= 68 && smb2_sniff(peek, n, &info) == 0 && info.valid) {
            LOG_INFO("sess=%llu smb %s cmd=%s", (unsigned long long)s->id,
                     smb2_dialect_name(info.dialect), smb2_command_name(info.command));
            s->logged_handshake = 1;
        }
    }
    uint8_t *buf = malloc(len);
    if (!buf) return;
    evbuffer_remove(in, buf, len);
    if (qs_stream_send(s->stream, buf, len) != 0) {
        LOG_WARN("sess=%llu stream send failed", (unsigned long long)s->id);
    } else {
        s->bytes_up += (uint64_t)len;
    }
    free(buf);
}

static void on_tcp_event(struct bufferevent *bev, short events, void *ctx) {
    (void)bev;
    qs_session_t *s = ctx;
    if (events & (BEV_EVENT_EOF | BEV_EVENT_ERROR | BEV_EVENT_TIMEOUT)) {
        // NOTE: do not free(s) here directly; stream side (reader thread)
        // may still hold the pointer. Mark dead, shut down stream, and let
        // stream on_close do the single free. If stream already gone, free now.
        if (s->stream) qs_stream_shutdown_write(s->stream);
        else if (!__sync_lock_test_and_set(&s->dead, 1)) {
            if (s->bev_tcp) { bufferevent_free(s->bev_tcp); s->bev_tcp = NULL; }
            free(s);
        }
    }
}

static void on_stream_data(qs_stream_t *st, const uint8_t *data, size_t len, void *ctx) {
    (void)st;
    qs_session_t *s = ctx;
    if (!s->bev_tcp) return;
    bufferevent_write(s->bev_tcp, data, len);
    s->bytes_down += (uint64_t)len;
}

static void on_stream_close(qs_stream_t *st, void *ctx) {
    (void)st;
    qs_session_t *s = ctx;
    tunnel_teardown(s);
    free(s);
}

qs_session_t *qs_tunnel_start(struct event_base *base,
                              struct bufferevent *bev_tcp,
                              struct qs_stream *stream,
                              qs_session_done_cb done_cb, void *ctx) {
    (void)base;
    (void)done_cb;
    (void)ctx;
    static uint64_t next_id = 1;
    qs_session_t *s = calloc(1, sizeof *s);
    if (!s) return NULL;
    s->id = __sync_fetch_and_add(&next_id, 1);
    s->bev_tcp = bev_tcp;
    s->stream = stream;
    bufferevent_setcb(bev_tcp, on_tcp_read, NULL, on_tcp_event, s);
    bufferevent_enable(bev_tcp, EV_READ | EV_WRITE);
    qs_stream_set_cbs(stream, (qs_stream_cbs_t){
                                  .on_data = on_stream_data,
                                  .on_close = on_stream_close,
                                  .ctx = s,
                              });
    return s;
}

void qs_tunnel_free(qs_session_t *s) {
    if (!s) return;
    tunnel_teardown(s);
    free(s);
}
