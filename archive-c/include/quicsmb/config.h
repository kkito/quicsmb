#ifndef QUICSMB_CONFIG_H
#define QUICSMB_CONFIG_H

#include "quicsmb/common.h"

typedef struct {
    qs_mode_t mode;              // client | server
    char server_host[256];       // client: quic server to dial
    int  server_port;            // client: quic server port (default 4433)
    char listen_host[64];        // client: local TCP listen addr
    int  listen_port;            // client: local TCP listen port (default 8445)
    int  quic_port;              // server: UDP listen port (default 4433)
    char backend_host[256];      // server: real SMB host
    int  backend_port;           // server: real SMB port (default 445)
    char cert_file[256];         // server: TLS cert (PEM)
    char key_file[256];          // server: TLS key (PEM)
    int  insecure_skip_verify;   // client: skip server cert verify (dev only)
    char alpn[32];               // default "quicsmb/1", set "smb" for Windows interop
    int  idle_timeout_sec;
    int  max_sessions;
} qs_config_t;

void qs_config_defaults(qs_config_t *c);
int  qs_config_load(const char *path, qs_config_t *c);

#endif
