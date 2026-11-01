// Stub QUIC backend: TCP passthrough with identical qs_quic API.
// Used when MsQuic is unavailable (dev/test). One stream == one TCP
// connection, so byte-stream semantics match QUIC bidi streams.
#include "quicsmb/quic.h"
#include "quicsmb/log.h"

#include <arpa/inet.h>
#include <errno.h>
#include <netdb.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

struct qs_quic {
    int is_server;
    char alpn[32];
    int listen_fd;
    int running;
    pthread_t accept_thr;
    qs_accept_cb accept_cb;
    void *accept_ctx;
};

struct qs_conn {
    qs_quic_t *q;
    char host[256];
    int port;
};

struct qs_stream {
    int fd;
    pthread_t rd_thr;
    int reader_started;
    int detached_reader;
    volatile int running;
    volatile int close_notified;
    qs_stream_cbs_t cbs;
    pthread_mutex_t wlock;
};

int qs_quic_has_real_quic(void) { return 0; }

static int tcp_connect(const char *host, int port) {
    struct addrinfo hints, *res = NULL;
    memset(&hints, 0, sizeof hints);
    hints.ai_socktype = SOCK_STREAM;
    char ps[16];
    snprintf(ps, sizeof ps, "%d", port);
    if (getaddrinfo(host, ps, &hints, &res) != 0) return -1;
    int fd = -1;
    for (struct addrinfo *r = res; r; r = r->ai_next) {
        fd = socket(r->ai_family, r->ai_socktype, r->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, r->ai_addr, r->ai_addrlen) == 0) break;
        close(fd);
        fd = -1;
    }
    freeaddrinfo(res);
    return fd;
}

static void *stream_reader(void *arg) {
    qs_stream_t *s = arg;
    uint8_t buf[QUICSMB_STREAM_BUF];
    while (s->running) {
        ssize_t n = recv(s->fd, buf, sizeof buf, 0);
        if (n > 0) {
            if (s->cbs.on_data) s->cbs.on_data(s, buf, (size_t)n, s->cbs.ctx);
        } else {
            break;
        }
    }
    // notify close exactly once
    if (!__sync_lock_test_and_set(&s->close_notified, 1)) {
        if (s->cbs.on_close) s->cbs.on_close(s, s->cbs.ctx);
    }
    return NULL;
}

static void stream_start_reader(qs_stream_t *s) {
    if (s->reader_started || !s->cbs.on_data) return;
    s->reader_started = 1;
    s->running = 1;
    pthread_create(&s->rd_thr, NULL, stream_reader, s);
    if (s->detached_reader) pthread_detach(s->rd_thr);
}

static qs_stream_t *stream_wrap_fd(int fd, qs_stream_cbs_t cbs) {
    qs_stream_t *s = calloc(1, sizeof *s);
    if (!s) return NULL;
    s->fd = fd;
    s->cbs = cbs;
    pthread_mutex_init(&s->wlock, NULL);
    // reader starts on set_cbs (server accept path) or now if cbs given
    stream_start_reader(s);
    return s;
}

int qs_quic_init_client(qs_quic_t **out, const char *alpn) {
    qs_quic_t *q = calloc(1, sizeof *q);
    if (!q) return -1;
    if (alpn) snprintf(q->alpn, sizeof q->alpn, "%s", alpn);
    *out = q;
    LOG_INFO("quic stub client init (alpn=%s) -- install msquic for real QUIC", q->alpn);
    return 0;
}

void qs_quic_set_insecure(qs_quic_t *q, int insecure) {
    (void)q;
    (void)insecure;
}

static void *accept_loop(void *arg) {
    qs_quic_t *q = arg;
    while (q->running) {
        int fd = accept(q->listen_fd, NULL, NULL);
        if (fd < 0) {
            if (q->running) LOG_WARN("stub accept: %s", strerror(errno));
            break;
        }
        qs_stream_t *s = stream_wrap_fd(fd, (qs_stream_cbs_t){0});
        if (!s) { close(fd); continue; }
        if (q->accept_cb) q->accept_cb(q, s, q->accept_ctx);
        else qs_stream_close(s);
    }
    return NULL;
}

int qs_quic_init_server(qs_quic_t **out, const char *alpn,
                        const char *cert_file, const char *key_file, int port) {
    (void)cert_file;
    (void)key_file;
    qs_quic_t *q = calloc(1, sizeof *q);
    if (!q) return -1;
    q->is_server = 1;
    if (alpn) snprintf(q->alpn, sizeof q->alpn, "%s", alpn);
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) { free(q); return -1; }
    int one = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);
    struct sockaddr_in a;
    memset(&a, 0, sizeof a);
    a.sin_family = AF_INET;
    a.sin_addr.s_addr = htonl(INADDR_ANY);
    a.sin_port = htons((uint16_t)port);
    if (bind(fd, (struct sockaddr *)&a, sizeof a) != 0 || listen(fd, 128) != 0) {
        LOG_ERROR("stub listen %d: %s", port, strerror(errno));
        close(fd);
        free(q);
        return -1;
    }
    q->listen_fd = fd;
    q->running = 1;
    pthread_create(&q->accept_thr, NULL, accept_loop, q);
    *out = q;
    LOG_INFO("quic stub server listening (tcp) on port %d alpn=%s", port, q->alpn);
    return 0;
}

void qs_quic_free(qs_quic_t *q) {
    if (!q) return;
    q->running = 0;
    if (q->is_server && q->listen_fd >= 0) {
        shutdown(q->listen_fd, SHUT_RDWR);
        close(q->listen_fd);
        pthread_join(q->accept_thr, NULL);
    }
    free(q);
}

qs_conn_t *qs_dial(qs_quic_t *q, const char *host, int port) {
    qs_conn_t *c = calloc(1, sizeof *c);
    if (!c) return NULL;
    c->q = q;
    snprintf(c->host, sizeof c->host, "%s", host ? host : "127.0.0.1");
    c->port = port;
    return c;
}

void qs_server_set_accept(qs_quic_t *q, qs_accept_cb cb, void *ctx) {
    q->accept_cb = cb;
    q->accept_ctx = ctx;
}

qs_stream_t *qs_stream_open(qs_conn_t *conn, qs_stream_cbs_t cbs) {
    int fd = tcp_connect(conn->host, conn->port);
    if (fd < 0) return NULL;
    return stream_wrap_fd(fd, cbs);
}

void qs_stream_set_cbs(qs_stream_t *s, qs_stream_cbs_t cbs) {
    if (!s) return;
    s->cbs = cbs;
    stream_start_reader(s);
}

int qs_stream_send(qs_stream_t *s, const uint8_t *data, size_t len) {
    if (!s || s->fd < 0) return -1;
    pthread_mutex_lock(&s->wlock);
    size_t off = 0;
    int rc = 0;
    while (off < len) {
        ssize_t n = send(s->fd, data + off, len - off, 0
#if defined(MSG_NOSIGNAL)
                         | MSG_NOSIGNAL
#endif
        );
        if (n <= 0) { rc = -1; break; }
        off += (size_t)n;
    }
    pthread_mutex_unlock(&s->wlock);
    return rc;
}

void qs_stream_shutdown_write(qs_stream_t *s) {
    if (s && s->fd >= 0) shutdown(s->fd, SHUT_WR);
}

void qs_stream_close(qs_stream_t *s) {
    if (!s) return;
    s->running = 0;
    if (s->fd >= 0) {
        shutdown(s->fd, SHUT_RDWR);
        close(s->fd);
        s->fd = -1;
    }
    if (s->reader_started && !s->detached_reader) {
        if (pthread_self() == s->rd_thr) {
            // called from reader thread itself (via on_close): detach
            pthread_detach(s->rd_thr);
            s->detached_reader = 1;
        } else {
            pthread_join(s->rd_thr, NULL);
        }
    }
    pthread_mutex_destroy(&s->wlock);
    free(s);
}
