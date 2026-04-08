/* Byzantine struct-by-value stress test.
 * Combines: struct params (all 3 ABI classes), struct returns, struct assignment,
 * nested field assignment, chained calls, recursive structs, and mixed registers.
 *
 * LP64 sizes (int=4 bytes):
 * Tag  = { int code }        =  4 bytes  (<=8:  1 reg in X0)
 * Vec2 = { int x; int y; }   =  8 bytes  (<=8:  1 reg in X0)
 * Mat2 = { Vec2 r0; Vec2 r1} = 16 bytes  (<=16: 2 regs X0+X1)
 */

struct Tag {
    int code;
};

struct Vec2 {
    int x;
    int y;
};

struct Mat2 {
    struct Vec2 r0;
    struct Vec2 r1;
};

/* ── helpers ──────────────────────────────────────────────────────── */

struct Vec2 make_vec2(int x, int y) {
    struct Vec2 v;
    v.x = x;
    v.y = y;
    return v;
}

struct Mat2 make_mat2(int ax, int ay, int bx, int by) {
    struct Mat2 m;
    m.r0.x = ax; m.r0.y = ay;
    m.r1.x = bx; m.r1.y = by;
    return m;
}

/* ── adversarial 1: large param + large return live at the same time ─
 * Callee receives Mat2 via pointer (>16) AND returns Mat2 via X8.
 * Both hidden pointers must be distinct and non-clobbered.               */
struct Mat2 transpose(struct Mat2 m) {
    struct Mat2 r;
    r.r0.x = m.r0.x;
    r.r0.y = m.r1.x;
    r.r1.x = m.r0.y;
    r.r1.y = m.r1.y;
    return r;
}

/* ── adversarial 2: chained call — return used as arg of same class ──
 * scale_vec2(add_vec2(v1,v2), tag): add_vec2 result is <=16, passed
 * directly to scale_vec2 without a named variable in between.            */
struct Vec2 add_vec2(struct Vec2 a, struct Vec2 b) {
    struct Vec2 r;
    r.x = a.x + b.x;
    r.y = a.y + b.y;
    return r;
}

struct Vec2 scale_vec2(struct Vec2 v, struct Tag s) {
    struct Vec2 r;
    r.x = v.x * s.code;
    r.y = v.y * s.code;
    return r;
}

/* ── adversarial 3: mixed small params + medium param, medium return ─
 * outer_product(Tag scale, Vec2 row, Vec2 col, Mat2 bias):
 *   r[i][j] = scale.code * row[i] * col[j] + bias[i][j]
 * LP64 register slots: X0(Tag=4) X1(Vec2 row=8) X2(Vec2 col=8) X3+X4(Mat2 bias=16)
 */
struct Mat2 outer_product(struct Tag scale, struct Vec2 row, struct Vec2 col, struct Mat2 bias) {
    struct Mat2 r;
    r.r0.x = scale.code * row.x * col.x + bias.r0.x;
    r.r0.y = scale.code * row.x * col.y + bias.r0.y;
    r.r1.x = scale.code * row.y * col.x + bias.r1.x;
    r.r1.y = scale.code * row.y * col.y + bias.r1.y;
    return r;
}

/* ── adversarial 4: double transpose — large return directly as large param ─
 * transpose(transpose(m)) must equal m.
 * The inner call returns via X8(dest=_sr0); the outer call receives _sr0
 * by pointer AND writes its result via a NEW X8.  Both X8 uses must
 * refer to different memory.                                              */

/* ── adversarial 5: nested field assignment then pass field as param ─── */
int vec2_dot(struct Vec2 a, struct Vec2 b) {
    return a.x * b.x + a.y * b.y;
}

/* ── adversarial 6: recursion with struct-by-value param ─────────────── */
int tag_sum(struct Tag t) {
    struct Tag next;
    if (t.code <= 0) {
        return 0;
    }
    next.code = t.code - 1;
    return t.code + tag_sum(next);
}

/* ── adversarial 7: two large params + large return ──────────────────── */
struct Mat2 mat_add(struct Mat2 a, struct Mat2 b) {
    struct Mat2 r;
    r.r0.x = a.r0.x + b.r0.x;
    r.r0.y = a.r0.y + b.r0.y;
    r.r1.x = a.r1.x + b.r1.x;
    r.r1.y = a.r1.y + b.r1.y;
    return r;
}

int main(void) {
    struct Tag  s1;
    struct Vec2 v1;
    struct Vec2 v2;
    struct Vec2 chained;
    struct Vec2 op_row;
    struct Vec2 op_col;
    struct Mat2 m1;
    struct Mat2 bias;
    struct Mat2 t1;
    struct Mat2 t2;
    struct Mat2 op;
    struct Mat2 sum;
    struct Tag  trec;

    s1.code = 2;
    v1 = make_vec2(3, 4);
    v2 = make_vec2(1, 2);
    m1 = make_mat2(1, 2, 3, 4);

    /* ── test 1: large param + large return ──────────────────────────── */
    /* m1 = [[1,2],[3,4]]; transpose → [[1,3],[2,4]]                      */
    t1 = transpose(m1);
    output(t1.r0.x);   /* 1 */
    output(t1.r0.y);   /* 3 */
    output(t1.r1.x);   /* 2 */
    output(t1.r1.y);   /* 4 */

    /* ── test 2: chained <=16 call-result as <=16 param ─────────────── */
    /* add_vec2({3,4},{1,2}) = {4,6}; scale_vec2({4,6},{code=2}) = {8,12} */
    chained = scale_vec2(add_vec2(v1, v2), s1);
    output(chained.x); /* 8  */
    output(chained.y); /* 12 */

    /* ── test 3: mixed small+medium+large params + large return ──────── */
    /* scale=2, row={3,4}, col={1,2}, bias=[[0,0],[0,0]]                  */
    /* r.r0.x = 2*3*1+0 = 6                                               */
    /* r.r0.y = 2*3*2+0 = 12                                              */
    /* r.r1.x = 2*4*1+0 = 8                                               */
    /* r.r1.y = 2*4*2+0 = 16                                              */
    bias = make_mat2(0, 0, 0, 0);
    op_row = make_vec2(3, 4);
    op_col = make_vec2(1, 2);
    op = outer_product(s1, op_row, op_col, bias);
    output(op.r0.x);   /* 6  */
    output(op.r0.y);   /* 12 */
    output(op.r1.x);   /* 8  */
    output(op.r1.y);   /* 16 */

    /* ── test 4: double transpose (large return → large param, chained) ─ */
    /* transpose(transpose([[1,2],[3,4]])) == [[1,2],[3,4]]                */
    t2 = transpose(transpose(m1));
    output(t2.r0.x);   /* 1 */
    output(t2.r0.y);   /* 2 */
    output(t2.r1.x);   /* 3 */
    output(t2.r1.y);   /* 4 */

    /* ── test 5: nested field assign then pass field as param ────────── */
    /* m1.r0 = v1 = {3,4}; m1.r1 stays {3,4}; dot = 3*3+4*4 = 25        */
    m1.r0 = v1;
    output(vec2_dot(m1.r0, m1.r1));  /* 25 */

    /* ── test 6: recursive struct-by-value param ─────────────────────── */
    /* tag_sum({5}) = 5+4+3+2+1 = 15                                      */
    trec.code = 5;
    output(tag_sum(trec));  /* 15 */

    /* ── test 7: two large params + large return ──────────────────────── */
    /* m1 = [[3,4],[3,4]] after test 5; transpose(m1) = [[3,3],[4,4]]     */
    /* mat_add: [[3+3,4+3],[3+4,4+4]] = [[6,7],[7,8]]                     */
    sum = mat_add(m1, transpose(m1));
    output(sum.r0.x);  /* 6 */
    output(sum.r0.y);  /* 7 */
    output(sum.r1.x);  /* 7 */
    output(sum.r1.y);  /* 8 */

    return 0;
}
