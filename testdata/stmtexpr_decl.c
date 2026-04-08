void main(void) {
    int v = 42;
    int r = ({ int t = v; t * 2; });
    output(r);
}
