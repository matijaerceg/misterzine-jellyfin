#include <assert.h>
#include "gateway_policy.h"
int main(void) {
    const char *root = "http://127.0.0.1:1234/secret/video/main.m3u8";
    assert(gateway_url_allowed(root, root));
    assert(gateway_url_allowed(root, "http://127.0.0.1:1234/secret/video/segment.ts"));
    assert(!gateway_url_allowed(root, "http://127.0.0.1:1235/secret/video/segment.ts"));
    assert(!gateway_url_allowed(root, "http://127.0.0.1:1234/other/video/segment.ts"));
    assert(!gateway_url_allowed(root, "http://127.0.0.1:1234@evil.test/secret/segment.ts"));
    assert(!gateway_url_allowed(root, "https://evil.test/key"));
    assert(!gateway_url_allowed(root, "file:/etc/passwd"));
    assert(!gateway_url_allowed(root, "crypto:http://evil.test/key"));
    assert(!gateway_url_allowed("http://127.0.0.1.evil.test:1234/secret/a", root));
    assert(!gateway_url_allowed("http://127.0.0.1:/secret/a", root));
    return 0;
}
