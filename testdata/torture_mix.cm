/* torture_mix.cm — byzantine combination of features 10/11/12/14
 *
 * Feature 10: designated initializers   { .field = val, ... }
 * Feature 11: anonymous struct/union members
 * Feature 12: adjacent string literal concatenation
 * Feature 14: compound literals          (Type){ ... }
 *
 * Struct layout (LP64, gaston natural alignment):
 *
 *   struct Vec2  { struct { int x; int y; }; }
 *     __anon_0 @ 0 (anon struct: x@0 y@4, size=8)
 *     sizeof = 8
 *
 *   struct Rect  { struct Vec2 origin; struct Vec2 size; int flags; }
 *     origin @ 0  (8 bytes)
 *     size   @ 8  (8 bytes)
 *     flags  @ 16 (int, 4 bytes)
 *     sizeof = 24
 *
 *   struct Tagged { int tag; union { int ival; long both; }; }
 *     tag      @  0  (int, 4)
 *     __anon_0 @  8  (anon union: ival@0 sz=4, both@0 sz=8; align→8)
 *       ival   @  8
 *       both   @  8
 *     sizeof = 16
 *
 * Expected output (27 lines):
 *   30 40 3 4 0 24 10 20 100 200 0 330 2500
 *   116 116 101 65 66 67
 *   7 42 42
 *   42
 *   10 20 30
 *   15
 */

/* ── struct definitions ────────────────────────────────────────────────── */

struct Vec2 {
    struct { int x; int y; };       /* anon inner struct — x/y promoted (11) */
};

struct Rect {
    struct Vec2 origin;
    struct Vec2 size;
    int flags;
};

struct Tagged {
    int tag;
    union {                         /* anon union — ival and both alias (11) */
        int  ival;
        long both;
    };
};

/* ── helpers ───────────────────────────────────────────────────────────── */

int dot(struct Vec2 *a, struct Vec2 *b) {
    return a->x * b->x + a->y * b->y;
}

int rect_sum(struct Rect *r) {
    return r->origin.x + r->origin.y + r->size.x + r->size.y + r->flags;
}

/* ── main: ALL declarations first, then all statements ─────────────────── */

void main(void) {
    /* declarations (10+11+12+14 exercised here) */
    struct Vec2 u     = { .y = 40, .x = 30 };                /* 10+11: out-of-order anon fields */
    struct Vec2 *vp   = &(struct Vec2){ .y = 4, .x = 3 };    /* 10+11+14: compound lit, out-of-order */
    struct Rect *rp   = &(struct Rect){                       /* 10+11+14: nested compound lit */
        .origin = { .x = 10, .y = 20 },
        .size   = { .x = 100, .y = 200 }
        /* flags omitted → zero-fill */
    };
    char *label       = "tor" "ture";                         /* 12: adjacent concat → "torture" */
    char *s2          = "A" "B" "C";                          /* 12: three singles → "ABC" */
    struct Tagged *tp = &(struct Tagged){ .tag = 7, .ival = 42 }; /* 10+11+14: anon union */
    int sv            = (int){ 42 };                          /* 14: scalar compound lit */
    int *arr          = (int[]){ 10, 20, 30 };                /* 14: array compound lit */

    /* ── section 1: anon-struct field access via named local (10+11) ── */
    output(u.x);   /* 30 */
    output(u.y);   /* 40 */

    /* ── section 2: anon-struct field access via compound-literal pointer (10+11+14) ── */
    output(vp->x); /* 3 */
    output(vp->y); /* 4 */

    /* ── section 3: compound literals as direct function arguments (10+11+14) ── */
    output(dot(&(struct Vec2){ .x = 1, .y = 0 },
               &(struct Vec2){ .x = 0, .y = 1 }));  /* 0  — orthogonal unit vectors */
    output(dot(&(struct Vec2){ .x = 3, .y = 4 },
               &(struct Vec2){ .x = 4, .y = 3 }));  /* 24 — 3*4+4*3=24 */

    /* ── section 4: nested compound literal with zero-fill (10+11+14) ── */
    output(rp->origin.x);  /* 10  */
    output(rp->origin.y);  /* 20  */
    output(rp->size.x);    /* 100 */
    output(rp->size.y);    /* 200 */
    output(rp->flags);     /* 0   — zero-fill: field not in compound lit */

    /* ── section 5: nested compound literal through helper (10+11+14) ── */
    output(rect_sum(rp));  /* 330 — 10+20+100+200+0 */

    /* ── section 6: named local through helper (11) ── */
    output(dot(&u, &u));   /* 2500 — 30²+40²=900+1600 */

    /* ── section 7: adjacent string concatenation — char indexing (12) ── */
    output(label[0]);   /* 't' = 116 */
    output(label[3]);   /* 't' = 116 */
    output(label[6]);   /* 'e' = 101 */
    output(s2[0]);      /* 'A' = 65  */
    output(s2[1]);      /* 'B' = 66  */
    output(s2[2]);      /* 'C' = 67  */

    /* ── section 8: anon-union aliasing via compound literal (10+11+14) ── */
    output(tp->tag);        /* 7  */
    output(tp->ival);       /* 42 */
    output((int)tp->both);  /* 42 — both aliases ival at same offset; high 32 bits zero-filled */

    /* ── section 9: scalar compound literal (14) ── */
    output(sv);   /* 42 */

    /* ── section 10: int-array compound literal indexed through pointer (14) ── */
    output(arr[0]);   /* 10 */
    output(arr[1]);   /* 20 */
    output(arr[2]);   /* 30 */

    /* ── section 11: compound literal arg with all fields out-of-order (10+11+14) ── */
    output(rect_sum(&(struct Rect){
        .flags  = 5,
        .size   = { .y = 2, .x = 1 },
        .origin = { .y = 4, .x = 3 }
    }));   /* 15 — 3+4+1+2+5 */
}
