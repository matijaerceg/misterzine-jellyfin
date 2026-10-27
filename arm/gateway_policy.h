#ifndef JF_GATEWAY_POLICY_H
#define JF_GATEWAY_POLICY_H
#include <string.h>
#include <ctype.h>

/* The gateway binds only IPv4 loopback. Keep nested media requests inside its
 * unguessable per-session path, including keys and initialization segments. */
static int gateway_url_allowed(const char *root, const char *url)
{
    const char *p, *path, *end;
    if (!root || !url || strncmp(root, "http://127.0.0.1:", 17)) return 0;
    p = root + 17;
    if (!isdigit((unsigned char)*p)) return 0;
    while (isdigit((unsigned char)*p)) ++p;
    if (*p != '/') return 0;
    path = p + 1;
    end = strchr(path, '/');
    if (!end || end == path) return 0;
    return strncmp(root, url, (size_t)(end - root + 1)) == 0;
}
#endif
