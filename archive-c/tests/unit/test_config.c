#include <cmocka.h>
#include <stdio.h>
#include <stdlib.h>
#include "quicsmb/config.h"

static void test_defaults(void **state) {
    (void)state;
    qs_config_t c;
    qs_config_defaults(&c);
    assert_int_equal(c.mode, QS_CLIENT);
    assert_int_equal(c.listen_port, QUICSMB_TCP_LISTEN_PORT);
    assert_int_equal(c.quic_port, QUICSMB_QUIC_PORT);
    assert_int_equal(c.backend_port, QUICSMB_SMB_PORT);
    assert_string_equal(c.alpn, QUICSMB_ALPN);
}

static void test_load(void **state) {
    (void)state;
    const char *path = "/tmp/qs_cfg_test.json";
    FILE *f = fopen(path, "w");
    assert_non_null(f);
    fputs("{\"mode\":\"server\",\"quic_port\":4433,\"backend_host\":\"10.0.0.1\",\"alpn\":\"smb\"}", f);
    fclose(f);
    qs_config_t c;
    assert_int_equal(qs_config_load(path, &c), 0);
    assert_int_equal(c.mode, QS_SERVER);
    assert_int_equal(c.quic_port, 4433);
    assert_string_equal(c.backend_host, "10.0.0.1");
    assert_string_equal(c.alpn, "smb");
    remove(path);
}

int main(void) {
    const struct CMUnitTest tests[] = {
        cmocka_unit_test(test_defaults),
        cmocka_unit_test(test_load),
    };
    return cmocka_run_group_tests(tests, NULL, NULL);
}
