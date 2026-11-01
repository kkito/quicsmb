// MsQuic backend: real IETF QUIC transport for SMB3 payload.
//
// One TCP connection <-> one QUIC bidi stream; streams are ordered byte
// streams, payload transparent (same contract as quic_stub.c).
//
// Threading model (PoC): MsQuic invokes callbacks on its own worker threads.
// We invoke the qs cbs directly from those threads; this is safe because the
// tunnel side uses BEV_OPT_THREADSAFE bufferevents and evthread_use_pthreads.
// A production version would marshal via eventfd into the libevent loop.
//
// Build: define QUICSMB_HAVE_MSQUIC=1 and link msquic (see CMakeLists).
#if QUICSMB_HAVE_MSQUIC

#include "quicsmb/quic.h"
#include "quicsmb/log.h"

#include <msquic.h>

#include <arpa/inet.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <netdb.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#ifndef QUIC_FAILED
#define QUIC_FAILED(s) ((s) != QUIC_STATUS_SUCCESS)
#endif

struct qs_quic {
    const QUIC_API_TABLE *api;
    HQUIC reg;
    HQUIC config; // client config or server config
    char alpn[32];
    int is_server;
    int insecure;
    char cert_file[256];
    char key_file[256];
    HQUIC listener;
    qs_accept_cb accept_cb;
    void *accept_ctx;
    pthread_mutex_t lock;
};

struct qs_conn {
    qs_quic_t *q;
    HQUIC conn;
    char host[256];
    int port;
    pthread_mutex_t lock;
    pthread_cond_t ready;
    volatile int connected;
    volatile int connect_failed;
    volatile int closed;
};

struct qs_stream {
    qs_quic_t *q;
    HQUIC stream;
    qs_stream_cbs_t cbs;
    pthread_mutex_t wlock;
    volatile int closed;
};

struct send_ctx {
    QUIC_BUFFER buf; // buf.Buffer points to data; both freed on SEND_COMPLETE
    uint8_t data[];
};

int qs_quic_has_real_quic(void) { return 1; }

// ---------------- stream callback ----------------
static QUIC_STATUS QUIC_API stream_cb(HQUIC stream, void *ctx, QUIC_STREAM_EVENT *ev) {
    qs_stream_t *s = (qs_stream_t *)ctx;
    (void)stream;
    // LOG_DEBUG("stream_cb ev=%d s=%p", (int)ev->Type, (void *)s); // noisy
    if (!s || s->closed) return QUIC_STATUS_SUCCESS;
    switch (ev->Type) {
    case QUIC_STREAM_EVENT_RECEIVE: {
        // IMPORTANT: returning SUCCESS tells MsQuic we consumed
        // TotalBufferLength bytes (it auto-completes). Do NOT call
        // StreamReceiveComplete inline here — that API is only for
        // deferred completion (return PENDING). Calling both double-
        // drains the recv buffer and trips quic_bugcheck -> abort.
        uint64_t total = ev->RECEIVE.TotalBufferLength;
        if (total > 0 && s->cbs.on_data) {
            // Copy first: the Buffers point into MsQuic's internal
            // recv chunks, which may be recycled after we return.
            uint8_t *buf = malloc((size_t)total);
            size_t off = 0;
            if (buf) {
                for (uint32_t i = 0; i < ev->RECEIVE.BufferCount; i++) {
                    uint32_t n = ev->RECEIVE.Buffers[i].Length;
                    memcpy(buf + off, ev->RECEIVE.Buffers[i].Buffer, n);
                    off += n;
                }
                s->cbs.on_data(s, buf, off, s->cbs.ctx);
                free(buf);
            }
        }
        if ((ev->RECEIVE.Flags & QUIC_RECEIVE_FLAG_FIN) && s->cbs.on_close) {
            s->cbs.on_close(s, s->cbs.ctx);
        }
        break;
    }
    case QUIC_STREAM_EVENT_SEND_COMPLETE: {
        // Single allocation (struct + payload); buffer memory is valid
        // until MsQuic fires this event.
        free(ev->SEND_COMPLETE.ClientContext);
        break;
    }
    case QUIC_STREAM_EVENT_PEER_SEND_SHUTDOWN:
    case QUIC_STREAM_EVENT_PEER_SEND_ABORTED:
    case QUIC_STREAM_EVENT_SHUTDOWN_COMPLETE:
        if (s->cbs.on_close) s->cbs.on_close(s, s->cbs.ctx);
        break;
    default:
        break;
    }
    return QUIC_STATUS_SUCCESS;
}

// ---------------- connection callback ----------------
static QUIC_STATUS QUIC_API conn_cb(HQUIC conn, void *ctx, QUIC_CONNECTION_EVENT *ev) {
    qs_conn_t *c = (qs_conn_t *)ctx;
    (void)conn;
    if (!c) {
        // server-accepted connection before ctx attached: handled in listener path
        return QUIC_STATUS_SUCCESS;
    }
    pthread_mutex_lock(&c->lock);
    switch (ev->Type) {
    case QUIC_CONNECTION_EVENT_CONNECTED:
        c->connected = 1;
        LOG_INFO("client: quic connection established");
        pthread_cond_broadcast(&c->ready);
        break;
    case QUIC_CONNECTION_EVENT_SHUTDOWN_INITIATED_BY_TRANSPORT:
        LOG_WARN("client: conn shutdown by transport status=%d connected=%d",
                 (int)ev->SHUTDOWN_INITIATED_BY_TRANSPORT.Status, (int)c->connected);
        if (!c->connected) {
            c->connect_failed = 1;
            pthread_cond_broadcast(&c->ready);
        }
        break;
    case QUIC_CONNECTION_EVENT_SHUTDOWN_INITIATED_BY_PEER:
        LOG_WARN("client: conn shutdown by peer err=%llu connected=%d",
                 (unsigned long long)ev->SHUTDOWN_INITIATED_BY_PEER.ErrorCode, (int)c->connected);
        if (!c->connected) {
            c->connect_failed = 1;
            pthread_cond_broadcast(&c->ready);
        }
        break;
    case QUIC_CONNECTION_EVENT_PEER_STREAM_STARTED: {
        // Server side: a peer opened a stream on our accepted connection.
        // Wrap it and hand to the accept callback.
        qs_stream_t *s = calloc(1, sizeof *s);
        if (!s) return QUIC_STATUS_SUCCESS;
        s->q = c->q;
        s->stream = ev->PEER_STREAM_STARTED.Stream;
        pthread_mutex_init(&s->wlock, NULL);
        c->q->api->SetCallbackHandler(s->stream, (void *)stream_cb, s);
        if (c->q->accept_cb) c->q->accept_cb(c->q, s, c->q->accept_ctx);
        else qs_stream_close(s);
        break;
    }
    case QUIC_CONNECTION_EVENT_SHUTDOWN_COMPLETE:
        c->closed = 1;
        break;
    default:
        break;
    }
    pthread_mutex_unlock(&c->lock);
    return QUIC_STATUS_SUCCESS;
}

// Server accepted-connection callback: connection arrives via listener with
// the qs_quic as context; attach config + swap handler to conn_cb.
// NOTE: must return SUCCESS (not PENDING) for every event, else MsQuic may
// stall the connection handshake.
static QUIC_STATUS QUIC_API server_conn_cb(HQUIC conn, void *ctx, QUIC_CONNECTION_EVENT *ev) {
    qs_quic_t *q = (qs_quic_t *)ctx;
    if (!q) return QUIC_STATUS_SUCCESS; // no ctx (e.g. after free)
    (void)conn;
    switch (ev->Type) {
    case QUIC_CONNECTION_EVENT_CONNECTED:
        LOG_INFO("server: quic connection established");
        break;
    case QUIC_CONNECTION_EVENT_PEER_STREAM_STARTED: {
        qs_stream_t *s = calloc(1, sizeof *s);
        if (!s) break;
        s->q = q;
        s->stream = ev->PEER_STREAM_STARTED.Stream;
        pthread_mutex_init(&s->wlock, NULL);
        q->api->SetCallbackHandler(s->stream, (void *)stream_cb, s);
        LOG_INFO("server: inbound stream opened");
        if (q->accept_cb) q->accept_cb(q, s, q->accept_ctx);
        else qs_stream_close(s);
        break;
    }
    case QUIC_CONNECTION_EVENT_SHUTDOWN_INITIATED_BY_TRANSPORT:
        LOG_WARN("server: conn shutdown by transport status=%d", (int)ev->SHUTDOWN_INITIATED_BY_TRANSPORT.Status);
        break;
    case QUIC_CONNECTION_EVENT_SHUTDOWN_INITIATED_BY_PEER:
        LOG_WARN("server: conn shutdown by peer err=%llu", (unsigned long long)ev->SHUTDOWN_INITIATED_BY_PEER.ErrorCode);
        break;
    case QUIC_CONNECTION_EVENT_SHUTDOWN_COMPLETE:
        break;
    default:
        break;
    }
    return QUIC_STATUS_SUCCESS;
}

static QUIC_STATUS QUIC_API listener_cb(HQUIC listener, void *ctx, QUIC_LISTENER_EVENT *ev) {
    qs_quic_t *q = (qs_quic_t *)ctx;
    (void)listener;
    LOG_DEBUG("listener event type=%d", (int)ev->Type);
    if (ev->Type == QUIC_LISTENER_EVENT_NEW_CONNECTION) {
        // Attach server config so the handshake proceeds, then watch streams.
        q->api->SetCallbackHandler(ev->NEW_CONNECTION.Connection, (void *)server_conn_cb, q);
        return q->api->ConnectionSetConfiguration(ev->NEW_CONNECTION.Connection, q->config);
    }
    return QUIC_STATUS_SUCCESS;
}

// ---------------- bootstrap ----------------
static int open_reg_config(qs_quic_t *q, int is_server, const char *cert, const char *key) {
    const QUIC_API_TABLE *api = NULL;
    if (QUIC_FAILED(MsQuicOpen2(&api))) {
        LOG_ERROR("MsQuicOpen2 failed");
        return -1;
    }
    q->api = api;
    QUIC_REGISTRATION_CONFIG reg_cfg = {"quicsmb", QUIC_EXECUTION_PROFILE_LOW_LATENCY};
    if (QUIC_FAILED(api->RegistrationOpen(&reg_cfg, &q->reg))) {
        LOG_ERROR("RegistrationOpen failed");
        return -1;
    }
    QUIC_BUFFER alpn;
    alpn.Length = (uint32_t)strlen(q->alpn);
    alpn.Buffer = (uint8_t *)q->alpn;
    // Allow the peer to open enough bidi streams (each TCP session uses one).
    // Default PeerBidiStreamCount is 0, which would block every StreamStart.
    QUIC_SETTINGS settings;
    memset(&settings, 0, sizeof settings);
    settings.PeerBidiStreamCount = 128;
    settings.IsSet.PeerBidiStreamCount = TRUE;
    if (QUIC_FAILED(api->ConfigurationOpen(q->reg, &alpn, 1, &settings, sizeof settings,
                                            NULL, &q->config))) {
        LOG_ERROR("ConfigurationOpen failed");
        return -1;
    }
    QUIC_CREDENTIAL_CONFIG cred;
    memset(&cred, 0, sizeof cred);
    QUIC_CERTIFICATE_FILE cert_file;
    if (is_server) {
        if (!cert || !cert[0] || !key || !key[0]) {
            LOG_ERROR("server mode requires cert_file + key_file");
            return -1;
        }
        cred.Type = QUIC_CREDENTIAL_TYPE_CERTIFICATE_FILE;
        cred.Flags = QUIC_CREDENTIAL_FLAG_NONE;
        cert_file.CertificateFile = cert;
        cert_file.PrivateKeyFile = key;
        cred.CertificateFile = &cert_file;
        if (QUIC_FAILED(api->ConfigurationLoadCredential(q->config, &cred))) {
            LOG_ERROR("server ConfigurationLoadCredential failed");
            return -1;
        }
    } else {
        cred.Type = QUIC_CREDENTIAL_TYPE_NONE;
        cred.Flags = QUIC_CREDENTIAL_FLAG_CLIENT;
        if (q->insecure) cred.Flags |= QUIC_CREDENTIAL_FLAG_NO_CERTIFICATE_VALIDATION;
        if (QUIC_FAILED(api->ConfigurationLoadCredential(q->config, &cred))) {
            LOG_ERROR("client ConfigurationLoadCredential failed");
            return -1;
        }
    }
    return 0;
}

int qs_quic_init_client(qs_quic_t **out, const char *alpn) {
    qs_quic_t *q = calloc(1, sizeof *q);
    if (!q) return -1;
    pthread_mutex_init(&q->lock, NULL);
    snprintf(q->alpn, sizeof q->alpn, "%s", alpn && alpn[0] ? alpn : QUICSMB_ALPN);
    q->insecure = 1; // dev default matches insecure_skip_verify; main overrides via config
    if (open_reg_config(q, 0, NULL, NULL) != 0) {
        free(q);
        return -1;
    }
    *out = q;
    LOG_INFO("quic msquic client init (alpn=%s)", q->alpn);
    return 0;
}

int qs_quic_init_server(qs_quic_t **out, const char *alpn,
                        const char *cert_file, const char *key_file, int port) {
    qs_quic_t *q = calloc(1, sizeof *q);
    if (!q) return -1;
    pthread_mutex_init(&q->lock, NULL);
    q->is_server = 1;
    snprintf(q->alpn, sizeof q->alpn, "%s", alpn && alpn[0] ? alpn : QUICSMB_ALPN);
    if (cert_file) snprintf(q->cert_file, sizeof q->cert_file, "%s", cert_file);
    if (key_file) snprintf(q->key_file, sizeof q->key_file, "%s", key_file);
    if (open_reg_config(q, 1, q->cert_file, q->key_file) != 0) {
        free(q);
        return -1;
    }
    if (QUIC_FAILED(q->api->ListenerOpen(q->reg, listener_cb, q, &q->listener))) {
        LOG_ERROR("ListenerOpen failed");
        return -1;
    }
    QUIC_ADDR addr;
    memset(&addr, 0, sizeof addr);
    struct sockaddr_in *v4 = (struct sockaddr_in *)&addr;
    v4->sin_family = AF_INET;
    v4->sin_port = htons((uint16_t)port);
    v4->sin_addr.s_addr = htonl(INADDR_ANY);
    QUIC_BUFFER alpn_buf;
    alpn_buf.Length = (uint32_t)strlen(q->alpn);
    alpn_buf.Buffer = (uint8_t *)q->alpn;
    if (QUIC_FAILED(q->api->ListenerStart(q->listener, &alpn_buf, 1, &addr))) {
        LOG_ERROR("ListenerStart :%d failed", port);
        return -1;
    }
    *out = q;
    LOG_INFO("quic msquic server listening on UDP :%d alpn=%s", port, q->alpn);
    return 0;
}

void qs_quic_free(qs_quic_t *q) {
    if (!q || !q->api) {
        free(q);
        return;
    }
    if (q->listener) {
        q->api->ListenerStop(q->listener);
        q->api->ListenerClose(q->listener);
    }
    if (q->config) q->api->ConfigurationClose(q->config);
    if (q->reg) {
        q->api->RegistrationShutdown(q->reg, QUIC_CONNECTION_SHUTDOWN_FLAG_SILENT, 0);
        q->api->RegistrationClose(q->reg);
    }
    pthread_mutex_destroy(&q->lock);
    free(q);
}

void qs_server_set_accept(qs_quic_t *q, qs_accept_cb cb, void *ctx) {
    q->accept_cb = cb;
    q->accept_ctx = ctx;
}

// NOTE: only meaningful before the client's ConfigurationLoadCredential.
// The public flow qs_quic_init_client() loads the credential eagerly with
// insecure=1 default, so this is a no-op for compatibility. Tests that need
// a verified peer must set q->insecure via qs_quic_init_client_insecure()
// (or accept the default) BEFORE init — see test_quic_stub.c.
void qs_quic_set_insecure(qs_quic_t *q, int insecure) {
    if (q) q->insecure = insecure ? 1 : 0;
}

// Variant honoring `insecure` (0 = verify server cert). Same as
// qs_quic_init_client but lets callers require validation.
int qs_quic_init_client_insecure(qs_quic_t **out, const char *alpn, int insecure) {
    qs_quic_t *q = calloc(1, sizeof *q);
    if (!q) return -1;
    pthread_mutex_init(&q->lock, NULL);
    snprintf(q->alpn, sizeof q->alpn, "%s", alpn && alpn[0] ? alpn : QUICSMB_ALPN);
    q->insecure = insecure ? 1 : 0;
    if (open_reg_config(q, 0, NULL, NULL) != 0) {
        free(q);
        return -1;
    }
    *out = q;
    LOG_INFO("quic msquic client init (alpn=%s insecure=%d)", q->alpn, q->insecure);
    return 0;
}

qs_conn_t *qs_dial(qs_quic_t *q, const char *host, int port) {
    qs_conn_t *c = calloc(1, sizeof *c);
    if (!c) return NULL;
    c->q = q;
    pthread_mutex_init(&c->lock, NULL);
    pthread_cond_init(&c->ready, NULL);
    snprintf(c->host, sizeof c->host, "%s", host ? host : "127.0.0.1");
    c->port = port;
    if (QUIC_FAILED(q->api->ConnectionOpen(q->reg, conn_cb, c, &c->conn))) {
        LOG_ERROR("ConnectionOpen failed");
        free(c);
        return NULL;
    }
    if (QUIC_FAILED(q->api->ConnectionStart(c->conn, q->config, QUIC_ADDRESS_FAMILY_UNSPEC,
                                            c->host, (uint16_t)port))) {
        LOG_ERROR("ConnectionStart %s:%d failed", c->host, port);
        q->api->ConnectionClose(c->conn);
        free(c);
        return NULL;
    }
    // Handshake is async: wait for CONNECTED (or shutdown) before streams.
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += 10;
    pthread_mutex_lock(&c->lock);
    while (!c->connected && !c->connect_failed) {
        if (pthread_cond_timedwait(&c->ready, &c->lock, &ts) != 0) break;
    }
    int ok = c->connected;
    pthread_mutex_unlock(&c->lock);
    if (!ok) {
        LOG_ERROR("quic handshake to %s:%d timed out/failed", c->host, port);
        q->api->ConnectionShutdown(c->conn, QUIC_CONNECTION_SHUTDOWN_FLAG_SILENT, 0);
        q->api->ConnectionClose(c->conn);
        free(c);
        return NULL;
    }
    return c;
}

qs_stream_t *qs_stream_open(qs_conn_t *conn, qs_stream_cbs_t cbs) {
    qs_stream_t *s = calloc(1, sizeof *s);
    if (!s) return NULL;
    s->q = conn->q;
    s->cbs = cbs;
    pthread_mutex_init(&s->wlock, NULL);
    // Never open streams on a dead/failed connection (avoids use-after-close).
    pthread_mutex_lock(&conn->lock);
    int alive = conn->connected && !conn->closed;
    pthread_mutex_unlock(&conn->lock);
    if (!alive) {
        LOG_WARN("StreamOpen on dead quic conn");
        pthread_mutex_destroy(&s->wlock);
        free(s);
        return NULL;
    }
    if (QUIC_FAILED(conn->q->api->StreamOpen(conn->conn, QUIC_STREAM_OPEN_FLAG_NONE,
                                             stream_cb, s, &s->stream))) {
        LOG_WARN("StreamOpen failed");
        pthread_mutex_destroy(&s->wlock);
        free(s);
        return NULL;
    }
    if (QUIC_FAILED(conn->q->api->StreamStart(s->stream, QUIC_STREAM_START_FLAG_NONE))) {
        LOG_WARN("StreamStart failed");
        conn->q->api->StreamClose(s->stream);
        pthread_mutex_destroy(&s->wlock);
        free(s);
        return NULL;
    }
    return s;
}

void qs_stream_set_cbs(qs_stream_t *s, qs_stream_cbs_t cbs) {
    if (s) s->cbs = cbs;
}

int qs_stream_send(qs_stream_t *s, const uint8_t *data, size_t len) {
    if (!s || s->closed || len == 0) return -1;
    // One allocation: MsQuic may hold the QUIC_BUFFERs past return
    // (async send path copies later via QuicStreamSendBufferRequest),
    // so the buffer must outlive this call — freed on SEND_COMPLETE.
    struct send_ctx *sc = malloc(sizeof *sc + len);
    if (!sc) return -1;
    memcpy(sc->data, data, len);
    sc->buf.Length = (uint32_t)len;
    sc->buf.Buffer = sc->data;
    pthread_mutex_lock(&s->wlock);
    int dead = s->closed;
    QUIC_STATUS st = dead ? QUIC_STATUS_ABORTED
                           : s->q->api->StreamSend(s->stream, &sc->buf, 1,
                                                   QUIC_SEND_FLAG_NONE, sc);
    pthread_mutex_unlock(&s->wlock);
    if (QUIC_FAILED(st)) {
        free(sc);
        return -1;
    }
    return 0;
}

void qs_stream_shutdown_write(qs_stream_t *s) {
    if (s && !s->closed)
        s->q->api->StreamShutdown(s->stream, QUIC_STREAM_SHUTDOWN_FLAG_GRACEFUL, 0);
}

void qs_stream_close(qs_stream_t *s) {
    if (!s) return;
    if (!__sync_lock_test_and_set(&s->closed, 1)) {
        s->q->api->StreamShutdown(s->stream, QUIC_STREAM_SHUTDOWN_FLAG_ABORT, 0);
        s->q->api->StreamClose(s->stream);
    }
    pthread_mutex_destroy(&s->wlock);
    free(s);
}

#else // !QUICSMB_HAVE_MSQUIC: keep TU empty; stub provides the symbols.
typedef int quicsmb_msquic_empty_t;
#endif
