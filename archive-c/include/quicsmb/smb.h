#ifndef QUICSMB_SMB_H
#define QUICSMB_SMB_H

// Minimal SMB2/SMB3 header sniffing: we treat the payload as opaque bytes,
// but parse just enough to log dialect + command (like rybaproxy logs method).
// See MS-SMB2 2.2.1 SMB2 Packet Header.

#include <stdint.h>
#include <stddef.h>

#define SMB2_PROTO_ID 0xFE534D42u // little-endian "\xFE SMB"

typedef struct {
    uint16_t dialect;   // from NEGOTIATE response (e.g. 0x0300/0x0302/0x0311)
    uint16_t command;   // SMB2 command code
    uint32_t msg_len;   // NetBIOS session length (type byte excluded)
    int      valid;
} smb2_info_t;

// Parse one NetBIOS Session Service + SMB2 header. Returns 0 on success.
int smb2_sniff(const uint8_t *buf, size_t len, smb2_info_t *out);

const char *smb2_command_name(uint16_t cmd);
const char *smb2_dialect_name(uint16_t dialect);

#endif
