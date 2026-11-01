// SMB2 sniff tests: build a fake NEGOTIATE packet and verify parser.
#include <cmocka.h>
#include <string.h>
#include "quicsmb/smb.h"

static void build_nbt_smb2(uint8_t *buf, size_t *len, uint16_t cmd, uint16_t dialect) {
    // NBT header
    buf[0] = 0x00;
    uint32_t body = 64 + 8;
    buf[1] = (body >> 16) & 0xFF;
    buf[2] = (body >> 8) & 0xFF;
    buf[3] = body & 0xFF;
    uint8_t *h = buf + 4;
    h[0] = 0xFE; h[1] = 'S'; h[2] = 'M'; h[3] = 'B';
    h[4] = 64; h[5] = 0; // StructureSize
    h[6] = 0; h[7] = 0; // credit charge
    h[8] = h[9] = h[10] = h[11] = 0; // status
    h[12] = cmd & 0xFF; h[13] = (cmd >> 8) & 0xFF;
    memset(h + 14, 0, 64 - 14);
    // fake negotiate-resp tail: ... DialectRevision at h+66
    memset(h + 64, 0, 8);
    h[66] = dialect & 0xFF; h[67] = (dialect >> 8) & 0xFF;
    *len = 4 + 64 + 8;
}

static void test_sniff_negotiate(void **state) {
    (void)state;
    uint8_t buf[128]; size_t len = 0;
    build_nbt_smb2(buf, &len, 0x0000, 0x0300);
    smb2_info_t info;
    assert_int_equal(smb2_sniff(buf, len, &info), 0);
    assert_true(info.valid);
    assert_int_equal(info.command, 0x0000);
    assert_string_equal(smb2_command_name(0x0000), "NEGOTIATE");
    assert_string_equal(smb2_dialect_name(0x0300), "SMB 3.0");
}

static void test_sniff_reject_garbage(void **state) {
    (void)state;
    uint8_t buf[128];
    memset(buf, 0x41, sizeof buf);
    smb2_info_t info;
    assert_int_not_equal(smb2_sniff(buf, sizeof buf, &info), 0);
    uint8_t tiny[4] = {0,0,0,0};
    assert_int_not_equal(smb2_sniff(tiny, sizeof tiny, &info), 0);
}

static void test_command_names(void **state) {
    (void)state;
    assert_string_equal(smb2_command_name(0x0005), "CREATE");
    assert_string_equal(smb2_command_name(0x0008), "READ");
    assert_string_equal(smb2_command_name(0x0009), "WRITE");
    assert_string_equal(smb2_command_name(0x0311 & 0xFFFF | 0xF000), "UNKNOWN");
    assert_string_equal(smb2_dialect_name(0x0311), "SMB 3.1.1");
}

int main(void) {
    const struct CMUnitTest tests[] = {
        cmocka_unit_test(test_sniff_negotiate),
        cmocka_unit_test(test_sniff_reject_garbage),
        cmocka_unit_test(test_command_names),
    };
    return cmocka_run_group_tests(tests, NULL, NULL);
}
