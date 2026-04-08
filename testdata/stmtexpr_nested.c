void main(void) {
    int r = ({ int a = ({ 4; }) + ({ 5; }); a + 1; });
    output(r);
}
