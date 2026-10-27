#include "tls_bridge.h"
int main(int argc, char **argv) {
    return argc == 4 && !strcmp(argv[1], "--tls-connect") ?
        tls_bridge_main(argv[2], argv[3]) : 2;
}
