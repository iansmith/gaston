/* computed_goto.c — GCC computed goto: &&label and goto *ptr */

void main(void) {
    void *tbl[3];
    tbl[0] = &&lbl_0;
    tbl[1] = &&lbl_1;
    tbl[2] = &&lbl_2;

    int i;
    for (i = 0; i < 3; i++) {
        goto *tbl[i];
lbl_0:
        output(100);
        if (i < 2) goto next;
        return;
lbl_1:
        output(200);
        if (i < 2) goto next;
        return;
lbl_2:
        output(300);
        return;
next:;
    }
}
