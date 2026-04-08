/* desinit_union.cm — single designator on a union (only one field written) */
union Val { int i; long l; };

void main(void) {
    union Val v = { .i = 42 };
    output(v.i);
    output(v.l);
}
