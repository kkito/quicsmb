#include "quicsmb/config.h"
#include "quicsmb/common.h"

#include <cjson/cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void copy_str(char *dst, size_t dstsz, const cJSON *obj, const char *key) {
    const cJSON *v = cJSON_GetObjectItemCaseSensitive(obj, key);
    if (cJSON_IsString(v) && v->valuestring) snprintf(dst, dstsz, "%s", v->valuestring);
}

static int get_int(const cJSON *obj, const char *key, int fb) {
    const cJSON *v = cJSON_GetObjectItemCaseSensitive(obj, key);
    return cJSON_IsNumber(v) ? v->valueint : fb;
}

static bool get_bool(const cJSON *obj, const char *key, bool fb) {
    const cJSON *v = cJSON_GetObjectItemCaseSensitive(obj, key);
    return cJSON_IsBool(v) ? cJSON_IsTrue(v) : fb;
}

void qs_config_defaults(qs_config_t *c) {
    memset(c, 0, sizeof *c);
    c->mode = QS_CLIENT;
    snprintf(c->server_host, sizeof c->server_host, "127.0.0.1");
    c->server_port = QUICSMB_QUIC_PORT;
    snprintf(c->listen_host, sizeof c->listen_host, "127.0.0.1");
    c->listen_port = QUICSMB_TCP_LISTEN_PORT;
    c->quic_port = QUICSMB_QUIC_PORT;
    snprintf(c->backend_host, sizeof c->backend_host, "127.0.0.1");
    c->backend_port = QUICSMB_SMB_PORT;
    c->idle_timeout_sec = 300;
    c->max_sessions = 1024;
    snprintf(c->alpn, sizeof c->alpn, "%s", QUICSMB_ALPN);
}

int qs_config_load(const char *path, qs_config_t *c) {
    qs_config_defaults(c);
    if (!path || !path[0]) return 0;
    FILE *f = fopen(path, "rb");
    if (!f) return -1;
    fseek(f, 0, SEEK_END);
    long sz = ftell(f);
    fseek(f, 0, SEEK_SET);
    if (sz < 0) { fclose(f); return -1; }
    char *buf = malloc((size_t)sz + 1);
    if (!buf) { fclose(f); return -1; }
    size_t rd = fread(buf, 1, (size_t)sz, f);
    buf[rd] = '\0';
    fclose(f);
    cJSON *root = cJSON_Parse(buf);
    free(buf);
    if (!root) return -1;

    char mode[16] = {0};
    copy_str(mode, sizeof mode, root, "mode");
    if (strcmp(mode, "server") == 0) c->mode = QS_SERVER;

    copy_str(c->server_host, sizeof c->server_host, root, "server_host");
    c->server_port = get_int(root, "server_port", c->server_port);
    copy_str(c->listen_host, sizeof c->listen_host, root, "listen_host");
    c->listen_port = get_int(root, "listen_port", c->listen_port);
    c->quic_port = get_int(root, "quic_port", c->quic_port);
    copy_str(c->backend_host, sizeof c->backend_host, root, "backend_host");
    c->backend_port = get_int(root, "backend_port", c->backend_port);
    copy_str(c->cert_file, sizeof c->cert_file, root, "cert_file");
    copy_str(c->key_file, sizeof c->key_file, root, "key_file");
    c->insecure_skip_verify = get_bool(root, "insecure_skip_verify", c->insecure_skip_verify);
    copy_str(c->alpn, sizeof c->alpn, root, "alpn");
    c->idle_timeout_sec = get_int(root, "idle_timeout_sec", c->idle_timeout_sec);
    c->max_sessions = get_int(root, "max_sessions", c->max_sessions);
    cJSON_Delete(root);
    return 0;
}
