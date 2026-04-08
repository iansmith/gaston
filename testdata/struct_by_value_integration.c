struct Score {
    int value;
};

struct Vec2 {
    int x;
    int y;
};

struct Triangle {
    struct Vec2 a;
    struct Vec2 b;
    struct Vec2 c;
};

struct Score make_score(int v) {
    struct Score s;
    s.value = v;
    return s;
}

struct Score add_scores(struct Score a, struct Score b) {
    struct Score r;
    r.value = a.value + b.value;
    return r;
}

struct Vec2 make_vec2(int x, int y) {
    struct Vec2 v;
    v.x = x;
    v.y = y;
    return v;
}

struct Vec2 add_vec2(struct Vec2 a, struct Vec2 b) {
    struct Vec2 r;
    r.x = a.x + b.x;
    r.y = a.y + b.y;
    return r;
}

struct Vec2 scale_vec2(struct Vec2 v, struct Score s) {
    struct Vec2 r;
    r.x = v.x * s.value;
    r.y = v.y * s.value;
    return r;
}

struct Triangle make_triangle(int ax, int ay, int bx, int by, int cx, int cy) {
    struct Triangle t;
    t.a.x = ax; t.a.y = ay;
    t.b.x = bx; t.b.y = by;
    t.c.x = cx; t.c.y = cy;
    return t;
}

int tri_centroid_x(struct Triangle t) {
    return (t.a.x + t.b.x + t.c.x) / 3;
}

int tri_centroid_y(struct Triangle t) {
    return (t.a.y + t.b.y + t.c.y) / 3;
}

struct Triangle translate_triangle(struct Triangle t, struct Vec2 delta) {
    struct Triangle r;
    r.a.x = t.a.x + delta.x; r.a.y = t.a.y + delta.y;
    r.b.x = t.b.x + delta.x; r.b.y = t.b.y + delta.y;
    r.c.x = t.c.x + delta.x; r.c.y = t.c.y + delta.y;
    return r;
}

int main(void) {
    struct Score s1;
    struct Score s2;
    struct Score s3;
    struct Vec2 v1;
    struct Vec2 v2;
    struct Vec2 v3;
    struct Vec2 sv;
    struct Triangle t;
    struct Triangle t2;

    s1 = make_score(3);
    s2 = make_score(7);
    s3 = add_scores(s1, s2);
    output(s3.value);        /* 10 */

    v1 = make_vec2(2, 4);
    v2 = make_vec2(6, 8);
    v3 = add_vec2(v1, v2);
    output(v3.x);            /* 8 */
    output(v3.y);            /* 12 */

    sv = scale_vec2(v1, s1);
    output(sv.x);            /* 6 */
    output(sv.y);            /* 12 */

    t = make_triangle(0, 0, 6, 0, 3, 9);
    output(tri_centroid_x(t));  /* 3 */
    output(tri_centroid_y(t));  /* 3 */

    t2 = translate_triangle(t, v1);
    output(t2.a.x);          /* 2 */
    output(t2.a.y);          /* 4 */
    output(t2.b.x);          /* 8 */
    output(t2.b.y);          /* 4 */
    output(t2.c.x);          /* 5 */
    output(t2.c.y);          /* 13 */

    s2 = s1;
    output(s2.value);        /* 3   (struct assign <=8) */
    v2 = v1;
    output(v2.x);            /* 2   (struct assign <=16) */
    t2 = t;
    output(t2.a.x);          /* 0   (struct assign >16; overwrites translate result) */

    return 0;
}
