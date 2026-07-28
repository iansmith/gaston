/* Named struct-typed array parameters, plain and C99 [static N] — the
 * musl lookup.h __lookup_serv/__lookup_name/__lookup_ipliteral shape:
 * struct service buf[static MAXSERVS]. Only CONST STRUCT ID ID '[' N ']'
 * existed before (const-qualified, no static variant, no plain named
 * non-const variant). */

struct point {
    int x;
};

int sum3(struct point pts[static 3]) {
    return pts[0].x + pts[1].x + pts[2].x;
}

int sum2(struct point pts[2]) {
    return pts[0].x + pts[1].x;
}

void main(void) {
    struct point arr[3];
    arr[0].x = 10;
    arr[1].x = 20;
    arr[2].x = 12;
    output(sum3(arr)); /* 42 */
    output(sum2(arr));  /* 30 */
}
