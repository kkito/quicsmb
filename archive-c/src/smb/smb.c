// SMB2 header sniffing (MS-SMB2 2.2.1). Payload stays opaque; we only peek.
#include "quicsmb/smb.h"
#include <string.h>

static uint16_t rd16le(const uint8_t *p) { return (uint16_t)(p[0] | (p[1] << 8)); }
static uint32_t rd32be_len(const uint8_t *p) {
    return ((uint32_t)p[1] << 16) | ((uint32_t)p[2] << 8) | (uint32_t)p[3];
}

int smb2_sniff(const uint8_t *buf, size_t len, smb2_info_t *out) {
    memset(out, 0, sizeof *out);
    // NetBIOS Session Service: 1 byte type + 3 bytes length
    if (len < 4 + 64) return -1;
    out->msg_len = rd32be_len(buf);
    const uint8_t *h = buf + 4;
    if (!(h[0] == 0xFE && h[1] == 'S' && h[2] == 'M' && h[3] == 'B')) return -1;
    // header: ProtocolId(4) StructureSize(2)=64 Command(2) ...
    uint16_t cmd;
    memcpy(&cmd, h + 12, 2);
    out->command = cmd;
    // dialect: for NEGOTIATE response (cmd 0x0000) at offset 68 => DialectRevision
    if (len >= 4 + 70) {
        uint16_t d;
        memcpy(&d, h + 64 + 2, 2); // Response DialectRevision position (approx; valid for NEGOTIATE resp)
        out->dialect = d;
    }
    out->valid = 1;
    (void)rd16le;
    return 0;
}

const char *smb2_command_name(uint16_t cmd) {
    switch (cmd) {
        case 0x0000: return "NEGOTIATE";
        case 0x0001: return "SESSION_SETUP";
        case 0x0002: return "LOGOFF";
        case 0x0003: return "TREE_CONNECT";
        case 0x0004: return "TREE_DISCONNECT";
        case 0x0005: return "CREATE";
        case 0x0006: return "CLOSE";
        case 0x0007: return "FLUSH";
        case 0x0008: return "READ";
        case 0x0009: return "WRITE";
        case 0x000A: return "LOCK";
        case 0x000B: return "IOCTL";
        case 0x000C: return "CANCEL";
        case 0x000D: return "ECHO";
        case 0x000E: return "QUERY_DIRECTORY";
        case 0x000F: return "CHANGE_NOTIFY";
        case 0x0010: return "QUERY_INFO";
        case 0x0011: return "SET_INFO";
        case 0x0012: return "OPLOCK_BREAK";
        default: return "UNKNOWN";
    }
}

const char *smb2_dialect_name(uint16_t d) {
    switch (d) {
        case 0x0202: return "SMB 2.0.2";
        case 0x0210: return "SMB 2.1";
        case 0x0300: return "SMB 3.0";
        case 0x0302: return "SMB 3.0.2";
        case 0x0311: return "SMB 3.1.1";
        default: return "SMB2/3";
    }
}
