/* SPDX-License-Identifier: GPL-3.0-or-later */
#include <assert.h>
#include <stdio.h>

#include "core_owner.h"

struct fake_cleanup { unsigned pause, settings; };

static void guarded_cleanup(struct jfplay_core_owner *owner, const char *name,
                            struct fake_cleanup *writes)
{
    if (!jfplay_core_owner_check(owner, name, 1, 99, 4000, 100)) return;
    ++writes->pause;
    ++writes->settings;
}

int main(void)
{
    assert(jfplay_core_name_owned("H264\n"));
    assert(jfplay_core_name_owned("MisterZine Jellyfin\r\n"));
    assert(!jfplay_core_name_owned(NULL));
    assert(!jfplay_core_name_owned(""));
    assert(!jfplay_core_name_owned("Other Core\n"));
    assert(!jfplay_core_name_owned("H264 extra\n"));

    struct jfplay_core_owner owner = {0};
    assert(jfplay_core_owner_check(&owner, "H264\n", 1, 10, 1000, 100));
    assert(!owner.lost);
    /* Pausing media leaves raster frames running, so advancing beat remains owned. */
    assert(jfplay_core_owner_check(&owner, "H264\n", 1, 11, 1050, 100));
    assert(jfplay_core_owner_check(&owner, "H264\n", 1, 12, 1100, 100));
    assert(!owner.lost);

    owner = (struct jfplay_core_owner){0};
    struct fake_cleanup writes = {0};
    guarded_cleanup(&owner, "Other Core\n", &writes);
    assert(owner.lost);
    assert(writes.pause == 0 && writes.settings == 0);
    guarded_cleanup(&owner, "MisterZine Jellyfin\n", &writes);
    assert(writes.pause == 0 && writes.settings == 0);

    owner = (struct jfplay_core_owner){0};
    assert(jfplay_core_owner_check(&owner, "MisterZine Jellyfin\n", 1, 7, 2000, 100));
    assert(jfplay_core_owner_check(&owner, "MisterZine Jellyfin\n", 1, 7, 2100, 100));
    assert(!jfplay_core_owner_check(&owner, "MisterZine Jellyfin\n", 1, 7, 2101, 100));
    writes = (struct fake_cleanup){0};
    guarded_cleanup(&owner, "MisterZine Jellyfin\n", &writes);
    assert(writes.pause == 0 && writes.settings == 0);

    owner = (struct jfplay_core_owner){0};
    assert(jfplay_core_owner_check(&owner, "H264\n", 0, 0, 3000, 100));
    assert(jfplay_core_owner_check(&owner, "H264\n", 0, 0, 3100, 100));
    assert(!jfplay_core_owner_check(&owner, "H264\n", 0, 0, 3101, 100));
    assert(owner.lost);

    puts("core ownership, raster heartbeat, and cleanup guard tests passed");
    return 0;
}
