/* SPDX-License-Identifier: GPL-3.0-or-later */
#ifndef JFPLAY_CORE_OWNER_H
#define JFPLAY_CORE_OWNER_H

#include <stdint.h>
#include <string.h>

/* Match MiSTer's first CORENAME line, including its optional CRLF ending. */
static inline int jfplay_core_name_owned(const char *name)
{
    size_t n;
    if (!name) return 0;
    n = strcspn(name, "\r\n");
    return (n == 4 && !memcmp(name, "H264", 4)) ||
           (n == 19 && !memcmp(name, "MisterZine Jellyfin", 19));
}

struct jfplay_core_owner {
    int lost, sampled, have_beat;
    uint32_t beat;
    uint64_t sampled_at_ms, beat_at_ms;
};

/* Once ownership is lost, never reclaim the mapping during this process.
 * A frame counter is required to keep writes enabled; advancing while paused
 * is expected because this counter follows raster frames, not decoded media. */
static inline int jfplay_core_owner_check(struct jfplay_core_owner *owner,
        const char *name, int beat_valid, uint32_t beat,
        uint64_t now_ms, uint64_t max_stale_ms)
{
    if (!owner || owner->lost) return 0;
    if (!jfplay_core_name_owned(name)) {
        owner->lost = 1;
        return 0;
    }
    if (!owner->sampled) {
        owner->sampled = 1;
        owner->sampled_at_ms = now_ms;
    }
    if (beat_valid) {
        if (!owner->have_beat || owner->beat != beat) {
            owner->beat = beat;
            owner->beat_at_ms = now_ms;
            owner->have_beat = 1;
        }
    }
    uint64_t last_alive = owner->have_beat ? owner->beat_at_ms : owner->sampled_at_ms;
    if (now_ms - last_alive > max_stale_ms) {
        owner->lost = 1;
        return 0;
    }
    return 1;
}

#endif
