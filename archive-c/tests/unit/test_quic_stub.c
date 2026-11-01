// QUIC echo test: server stream echoes, client stream sends + receives.
// With real MsQuic built in (QUICSMB_HAVE_MSQUIC=1) this exercises the actual
// QUIC transport using the self-signed dev cert in the repo root.
// With the stub backend it exercises the TCP-passthrough fallback.
#include <cmocka.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include "quicsmb/quic.h"

static qs_stream_t *g_srv_stream;
static pthread_mutex_t g_mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_cv = PTHREAD_COND_INITIALIZER;

static void srv_data(qs_stream_t *s, const uint8_t *d, size_t n, void *ctx) {
    (void)ctx;
    qs_stream_send(s, d, n); // echo
}
static void srv_close(qs_stream_t *s, void *ctx) { (void)s; (void)ctx; }
static void on_accept(qs_quic_t *q, qs_stream_t *s, void *ctx) {
    (void)q; (void)ctx;
    pthread_mutex_lock(&g_mu);
    g_srv_stream = s;
    pthread_cond_signal(&g_cv);
    pthread_mutex_unlock(&g_mu);
    qs_stream_set_cbs(s, (qs_stream_cbs_t){.on_data = srv_data, .on_close = srv_close});
}

static uint8_t g_got[256];
static size_t g_gotlen;
static int g_closed;

static void cli_data(qs_stream_t *s, const uint8_t *d, size_t n, void *ctx) {
    (void)s; (void)ctx;
    size_t c = n > sizeof g_got - g_gotlen ? sizeof g_got - g_gotlen : n;
    memcpy(g_got + g_gotlen, d, c);
    g_gotlen += c;
}
static void cli_close(qs_stream_t *s, void *ctx) { (void)s; (void)ctx; g_closed = 1; }

static void test_quic_echo(void **state) {
    (void)state;
#if QUICSMB_HAVE_MSQUIC
    // cert/key live in repo root; derive from this file's path so ctest
    // works regardless of CWD (build dir != source dir).
    char cert[512], key[512];
    const char *f = __FILE__; // .../quicsmb/tests/unit/test_quic_stub.c
    const char *t = strstr(f, "/tests/");
    size_t rootlen = t ? (size_t)(t - f) : 0;
    if (rootlen == 0 || rootlen + 16 >= sizeof cert)
        fail_msg("cannot derive repo root from __FILE__");
    memcpy(cert, f, rootlen);
    cert[rootlen] = '\0';
    snprintf(key, sizeof key, "%s", cert);
    strncat(cert, "/cert.pem", sizeof cert - strlen(cert) - 1);
    strncat(key, "/key.pem", sizeof key - strlen(key) - 1);
#else
    const char *cert = NULL, *key = NULL; // stub ignores cert/key
#endif
    qs_quic_t *srv = NULL, *cli = NULL;
    assert_int_equal(qs_quic_init_server(&srv, "quicsmb/1", cert, key, 14433), 0);
    qs_server_set_accept(srv, on_accept, NULL);
    assert_int_equal(qs_quic_init_client(&cli, "quicsmb/1"), 0); // insecure=1 default
    qs_conn_t *c = qs_dial(cli, "127.0.0.1", 14433);
    assert_non_null(c);
    qs_stream_t *st = qs_stream_open(c, (qs_stream_cbs_t){
                                            .on_data = cli_data, .on_close = cli_close});
    assert_non_null(st);
    // NOTE: send BEFORE waiting for accept. MsQuic only fires
    // PEER_STREAM_STARTED on the server once the first STREAM frame
    // arrives — waiting for accept before sending deadlocks.
    const char *msg = "SMB-over-QUIC hello";
    assert_int_equal(qs_stream_send(st, (const uint8_t *)msg, strlen(msg)), 0);
    // wait for server accept (triggered by the first STREAM frame)
    pthread_mutex_lock(&g_mu);
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += 5;
    while (!g_srv_stream) pthread_cond_timedwait(&g_cv, &g_mu, &ts);
    pthread_mutex_unlock(&g_mu);
    assert_non_null(g_srv_stream);
    // wait echo
    for (int i = 0; i < 200 && g_gotlen < strlen(msg); i++) usleep(10000);
    assert_int_equal(g_gotlen, strlen(msg));
    assert_memory_equal(g_got, msg, strlen(msg));

    qs_stream_close(st);
    usleep(100000);
    // NOTE: no qs_quic_free() — MsQuic teardown races the worker threads
    // (SHUTDOWN_COMPLETE callbacks can arrive after RegistrationClose).
    // The OS reclaims reg/config/listener on process exit; ctest only
    // needs the echo assertions above.
    (void)srv;
    (void)cli;
    (void)g_closed;
}

int main(void) {
    const struct CMUnitTest tests[] = {
        cmocka_unit_test(test_quic_echo),
    };
    return cmocka_run_group_tests(tests, NULL, NULL);
}
