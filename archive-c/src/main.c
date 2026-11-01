// quicsmb: SMB3 over QUIC tunnel (C, libevent + MsQuic).
//
// client: listen TCP (default 127.0.0.1:8445) -> one QUIC bidi stream each.
// server: accept QUIC streams -> dial backend SMB (default 127.0.0.1:445).
// Payload is transparent bytes; SMB2 header is only sniffed for logging.
#include "quicsmb/common.h"
#include "quicsmb/config.h"
#include "quicsmb/log.h"
#include "quicsmb/quic.h"
#include "quicsmb/tunnel.h"

#include <event2/event.h>
#include <event2/listener.h>
#include <event2/bufferevent.h>
#include <event2/thread.h>

#include <arpa/inet.h>
#include <signal.h>
#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static struct event_base *g_base;
static qs_quic_t *g_quic;
static qs_config_t g_cfg;
static qs_conn_t *g_conn; // client persistent conn template
static uint64_t g_sessions;

static void on_signal(int sig) {
    (void)sig;
    if (g_base) event_base_loopbreak(g_base);
}

static void keepalive_cb(evutil_socket_t fd, short what, void *ctx) {
    (void)fd;
    (void)what;
    (void)ctx;
}

// ---------------- client: TCP accept -> QUIC stream ----------------
static void client_accept_cb(struct evconnlistener *l, evutil_socket_t fd,
                             struct sockaddr *a, int alen, void *ctx) {
    (void)l;
    (void)a;
    (void)alen;
    (void)ctx;
    if ((uint64_t)g_cfg.max_sessions > 0 && g_sessions >= (uint64_t)g_cfg.max_sessions) {
        LOG_WARN("max_sessions reached, rejecting");
        close((int)fd);
        return;
    }
    struct bufferevent *bev = bufferevent_socket_new(
        g_base, fd, BEV_OPT_CLOSE_ON_FREE | BEV_OPT_THREADSAFE);
    if (!bev) {
        close((int)fd);
        return;
    }
    qs_stream_t *st = qs_stream_open(g_conn, (qs_stream_cbs_t){0});
    if (!st) {
        LOG_WARN("dial quic %s:%d failed", g_cfg.server_host, g_cfg.server_port);
        bufferevent_free(bev);
        return;
    }
    g_sessions++;
    qs_session_t *s = qs_tunnel_start(g_base, bev, st, NULL, NULL);
    if (!s) {
        qs_stream_close(st);
        bufferevent_free(bev);
        g_sessions--;
        return;
    }
    LOG_DEBUG("client sess=%llu started", (unsigned long long)s->id);
}

static int run_client(void) {
    if (qs_quic_init_client(&g_quic, g_cfg.alpn) != 0) return 1;
    qs_quic_set_insecure(g_quic, g_cfg.insecure_skip_verify);
    g_conn = qs_dial(g_quic, g_cfg.server_host, g_cfg.server_port);
    if (!g_conn) return 1;

    struct sockaddr_in sin;
    memset(&sin, 0, sizeof sin);
    sin.sin_family = AF_INET;
    sin.sin_port = htons((uint16_t)g_cfg.listen_port);
    if (g_cfg.listen_host[0] && strcmp(g_cfg.listen_host, "0.0.0.0") != 0) {
        if (inet_pton(AF_INET, g_cfg.listen_host, &sin.sin_addr) != 1) {
            LOG_ERROR("bad listen_host %s", g_cfg.listen_host);
            return 1;
        }
    }
    struct evconnlistener *lis = evconnlistener_new_bind(
        g_base, client_accept_cb, NULL, LEV_OPT_REUSEABLE | LEV_OPT_CLOSE_ON_FREE, 128,
        (struct sockaddr *)&sin, sizeof sin);
    if (!lis) {
        LOG_ERROR("listen %s:%d failed", g_cfg.listen_host, g_cfg.listen_port);
        return 1;
    }
    LOG_INFO("quicsmb client: tcp %s:%d -> quic %s:%d alpn=%s (%s)", g_cfg.listen_host,
             g_cfg.listen_port, g_cfg.server_host, g_cfg.server_port, g_cfg.alpn,
             qs_quic_has_real_quic() ? "msquic" : "stub");
    struct event *cli_keep = event_new(g_base, -1, EV_PERSIST, keepalive_cb, NULL);
    struct timeval cli_hour = {3600, 0};
    event_add(cli_keep, &cli_hour);
    event_base_dispatch(g_base);
    event_free(cli_keep);
    evconnlistener_free(lis);
    return 0;
}

// ---------------- server: QUIC stream -> backend TCP ----------------
static void server_accept_cb(qs_quic_t *q, qs_stream_t *s, void *ctx) {
    (void)q;
    (void)ctx;
    struct bufferevent *bev = bufferevent_socket_new(
        g_base, -1, BEV_OPT_CLOSE_ON_FREE | BEV_OPT_THREADSAFE);
    if (!bev) {
        qs_stream_close(s);
        return;
    }
    // Start the tunnel immediately (before backend TCP connects) so the
    // first RECEIVE that arrives on the MsQuic worker thread is not
    // dropped: bufferevent_write() buffers until connect completes, and
    // tunnel's on_tcp_event tears everything down on dial failure/EOF.
    LOG_DEBUG("server: inbound stream %p", (void *)s);
    qs_session_t *sess = qs_tunnel_start(g_base, bev, s, NULL, NULL);
    if (!sess) {
        bufferevent_free(bev);
        qs_stream_close(s);
        return;
    }
    g_sessions++;
    LOG_DEBUG("server sess=%llu accepted", (unsigned long long)sess->id);
    struct sockaddr_in sin;
    memset(&sin, 0, sizeof sin);
    sin.sin_family = AF_INET;
    sin.sin_port = htons((uint16_t)g_cfg.backend_port);
    inet_pton(AF_INET, g_cfg.backend_host, &sin.sin_addr);
    if (bufferevent_socket_connect(bev, (struct sockaddr *)&sin, sizeof sin) != 0) {
        LOG_WARN("backend connect %s:%d failed", g_cfg.backend_host, g_cfg.backend_port);
        qs_tunnel_free(sess);
        g_sessions--;
    }
}

static int run_server(void) {
    if (qs_quic_init_server(&g_quic, g_cfg.alpn, g_cfg.cert_file[0] ? g_cfg.cert_file : NULL,
                            g_cfg.key_file[0] ? g_cfg.key_file : NULL, g_cfg.quic_port) != 0)
        return 1;
    qs_server_set_accept(g_quic, server_accept_cb, NULL);
    // stub backend accept runs on its own thread; keep the libevent loop
    // alive (it drives backend bufferevents) with a persistent timer.
    struct event *keep = event_new(g_base, -1, EV_PERSIST, keepalive_cb, NULL);
    struct timeval hour = {3600, 0};
    event_add(keep, &hour);
    LOG_INFO("quicsmb server: quic :%d alpn=%s -> smb %s:%d (%s)", g_cfg.quic_port, g_cfg.alpn,
             g_cfg.backend_host, g_cfg.backend_port,
             qs_quic_has_real_quic() ? "msquic" : "stub");
    event_base_dispatch(g_base);
    event_free(keep);
    return 0;
}

static void usage(const char *p) {
    fprintf(stderr,
            "usage: %s [-c config.json] [-m client|server] [-v]\n"
            "  client: -m client (listen tcp, dial quic)\n"
            "  server: -m server (listen quic, dial backend smb)\n",
            p);
}

int main(int argc, char **argv) {
    const char *cfg_path = NULL;
    const char *mode = NULL;
    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "-c") == 0 && i + 1 < argc) cfg_path = argv[++i];
        else if (strcmp(argv[i], "-m") == 0 && i + 1 < argc) mode = argv[++i];
        else if (strcmp(argv[i], "-v") == 0) {
            printf("quicsmb 0.1.0\n");
            return 0;
        } else {
            usage(argv[0]);
            return 1;
        }
    }
    qs_config_defaults(&g_cfg);
    if (cfg_path && qs_config_load(cfg_path, &g_cfg) != 0) {
        fprintf(stderr, "config load failed: %s\n", cfg_path);
        return 1;
    }
    if (mode) g_cfg.mode = (strcmp(mode, "server") == 0) ? QS_SERVER : QS_CLIENT;

    log_init(LOG_INFO, NULL);
    const char *qs_debug = getenv("QS_DEBUG");
    if (qs_debug && qs_debug[0] && strcmp(qs_debug, "0") != 0)
        log_set_level(LOG_DEBUG);
    evthread_use_pthreads();
    g_base = event_base_new();
    if (!g_base) {
        LOG_ERROR("event_base_new failed");
        return 1;
    }
    signal(SIGINT, on_signal);
    signal(SIGTERM, on_signal);

    int rc = (g_cfg.mode == QS_SERVER) ? run_server() : run_client();
    qs_quic_free(g_quic);
    event_base_free(g_base);
    return rc;
}
