#ifndef JF_TLS_BRIDGE_H
#define JF_TLS_BRIDGE_H
/* A private stdio connection for the Go HTTP transport. No HTTP parsing,
 * redirects, credentials or FPGA access here. Verify before reading stdin. */
#include <openssl/ssl.h>
#include <openssl/x509v3.h>
#include <arpa/inet.h>
#include <netdb.h>
#include <poll.h>
#include <fcntl.h>
#include <unistd.h>
#include <errno.h>
#include <signal.h>
#include <stdlib.h>
#include <string.h>

static int tls_bridge_wait(int fd, short events)
{
    struct pollfd p = {fd, events, 0};
    int n;
    do { n = poll(&p, 1, 10000); } while (n < 0 && errno == EINTR);
    return n > 0 && !(p.revents & (POLLERR | POLLNVAL)) ? 0 : -1;
}

static int tls_bridge_main(const char *host, const char *port)
{
    struct addrinfo hints = {0}, *addresses = NULL, *a;
    SSL_CTX *ctx = NULL;
    SSL *ssl = NULL;
    unsigned char ip[16], in[16384], out[16384];
    size_t in_len = 0, out_len = 0, out_at = 0;
    short read_want = POLLIN, write_want = POLLOUT;
    int fd = -1, result = 1, eof = 0;
    char *end;
    long number = strtol(port, &end, 10);
    if (!host[0] || !port[0] || *end || number < 1 || number > 65535) return 2;
    signal(SIGPIPE, SIG_IGN);
    hints.ai_socktype = SOCK_STREAM;
    hints.ai_family = AF_UNSPEC;
    if (getaddrinfo(host, port, &hints, &addresses)) goto done;
    for (a = addresses; a; a = a->ai_next) {
        fd = socket(a->ai_family, a->ai_socktype, a->ai_protocol);
        if (fd < 0) continue;
        if (fcntl(fd, F_SETFL, O_NONBLOCK) == 0) {
            int rc = connect(fd, a->ai_addr, a->ai_addrlen);
            if (!rc) break;
            if (errno == EINPROGRESS && !tls_bridge_wait(fd, POLLOUT)) {
                int error = 0;
                socklen_t len = sizeof error;
                if (!getsockopt(fd, SOL_SOCKET, SO_ERROR, &error, &len) && !error) break;
            }
        }
        close(fd); fd = -1;
    }
    if (fd < 0) goto done;
    ctx = SSL_CTX_new(TLS_client_method());
    const char *ca_file = getenv("SSL_CERT_FILE"), *ca_dir = getenv("SSL_CERT_DIR");
    if (!ca_dir || !*ca_dir) ca_dir = "/etc/ssl/certs";
    if (!ctx || !SSL_CTX_set_min_proto_version(ctx, TLS1_2_VERSION)) goto done;
    /* Match the system bundle locations used by Go, including MiSTer Linux. */
    if (ca_file && *ca_file) {
        if (!SSL_CTX_load_verify_locations(ctx, ca_file, NULL)) goto done;
    } else {
        const char *bundles[] = {
            "/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt",
            "/etc/ssl/ca-bundle.pem", "/etc/pki/tls/cacert.pem",
            "/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", "/etc/ssl/cert.pem"
        };
        for (size_t i = 0; i < sizeof bundles / sizeof bundles[0]; ++i)
            if (!access(bundles[i], R_OK) && SSL_CTX_load_verify_locations(ctx, bundles[i], NULL)) break;
    }
    if (!SSL_CTX_load_verify_locations(ctx, NULL, ca_dir)) goto done;
    SSL_CTX_set_verify(ctx, SSL_VERIFY_PEER, NULL);
    ssl = SSL_new(ctx);
    if (!ssl || !SSL_set_fd(ssl, fd)) goto done;
    X509_VERIFY_PARAM *verify = SSL_get0_param(ssl);
    X509_VERIFY_PARAM_set_hostflags(verify,
        X509_CHECK_FLAG_NO_PARTIAL_WILDCARDS | X509_CHECK_FLAG_NEVER_CHECK_SUBJECT);
    if (inet_pton(AF_INET, host, ip) == 1 || inet_pton(AF_INET6, host, ip) == 1) {
        if (!X509_VERIFY_PARAM_set1_ip_asc(verify, host)) goto done;
    } else {
        if (!SSL_set_tlsext_host_name(ssl, host) ||
            !X509_VERIFY_PARAM_set1_host(verify, host, 0)) goto done;
    }
    for (;;) {
        int n = SSL_connect(ssl);
        if (n == 1) break;
        int error = SSL_get_error(ssl, n);
        if (error != SSL_ERROR_WANT_READ && error != SSL_ERROR_WANT_WRITE) goto done;
        if (tls_bridge_wait(fd, error == SSL_ERROR_WANT_READ ? POLLIN : POLLOUT)) goto done;
    }
    if (SSL_get_verify_result(ssl) != X509_V_OK) goto done;
    if (fcntl(STDIN_FILENO, F_SETFL, O_NONBLOCK) ||
        fcntl(STDOUT_FILENO, F_SETFL, O_NONBLOCK)) goto done;
    for (;;) {
        struct pollfd fds[3] = {
            {STDIN_FILENO, !in_len && !eof ? POLLIN : 0, 0},
            {STDOUT_FILENO, out_len ? POLLOUT : 0, 0},
            {fd, (short)((!out_len && !eof ? read_want : 0) | (in_len ? write_want : 0)), 0}
        };
        int pending = !out_len && !eof && SSL_pending(ssl);
        int n = poll(fds, 3, pending ? 0 : -1);
        if (n < 0) { if (errno == EINTR) continue; goto done; }
        if (!in_len && (fds[0].revents & (POLLIN | POLLHUP))) {
            ssize_t count = read(STDIN_FILENO, in, sizeof in);
            if (!count) { result = 0; goto done; }
            if (count > 0) in_len = (size_t)count;
            else if (errno != EAGAIN && errno != EINTR) goto done;
        }
        if (in_len && (fds[2].revents & write_want)) {
            n = SSL_write(ssl, in, (int)in_len);
            if (n > 0) {
                memmove(in, in + n, in_len - (size_t)n);
                in_len -= (size_t)n; write_want = POLLOUT;
            } else {
                int error = SSL_get_error(ssl, n);
                if (error != SSL_ERROR_WANT_READ && error != SSL_ERROR_WANT_WRITE) goto done;
                write_want = error == SSL_ERROR_WANT_READ ? POLLIN : POLLOUT;
            }
        }
        if (!out_len && !eof && (pending || (fds[2].revents & (read_want | POLLHUP)))) {
            n = SSL_read(ssl, out, sizeof out);
            if (n > 0) { out_len = (size_t)n; out_at = 0; read_want = POLLIN; }
            else {
                int error = SSL_get_error(ssl, n);
                if (error == SSL_ERROR_ZERO_RETURN) eof = 1;
                else if (error == SSL_ERROR_WANT_READ) read_want = POLLIN;
                else if (error == SSL_ERROR_WANT_WRITE) read_want = POLLOUT;
                else goto done;
            }
        }
        if (out_len && (fds[1].revents & POLLOUT)) {
            ssize_t count = write(STDOUT_FILENO, out + out_at, out_len);
            if (count > 0) { out_at += (size_t)count; out_len -= (size_t)count; }
            else if (count < 0 && errno != EAGAIN && errno != EINTR) goto done;
        }
        if (eof && !out_len) { result = 0; goto done; }
        if ((fds[0].revents | fds[1].revents | fds[2].revents) & (POLLERR | POLLNVAL)) goto done;
    }
done:
    SSL_free(ssl);
    SSL_CTX_free(ctx);
    if (fd >= 0) close(fd);
    if (addresses) freeaddrinfo(addresses);
    return result;
}
#endif
