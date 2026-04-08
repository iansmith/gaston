/* struct_value.cm — struct-by-value fields (item 3).
   struct Point { int x; int y; } — x@0(4), y@4(4), size=8.
   struct Rect { Point tl; Point br; } — tl@0(8), br@8(8), size=16.
   Verifies: correct byte offsets, recursive SizeBytes, chained dot access. */
struct Point { int x; int y; };
struct Rect { struct Point tl; struct Point br; };

void main(void) {
    struct Rect r;
    r.tl.x = 1;
    r.tl.y = 2;
    r.br.x = 10;
    r.br.y = 20;
    output(r.tl.x);
    output(r.tl.y);
    output(r.br.x);
    output(r.br.y);
    output(r.br.x + r.tl.y);
    output(sizeof(struct Point));
    output(sizeof(struct Rect));
}
