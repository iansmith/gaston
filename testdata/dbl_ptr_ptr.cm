void set_val(double **pp, double v) {
    **pp = v;
}

int main(void) {
    double x;
    double y;
    double *p;
    double **pp;
    p = &x;
    pp = &p;
    set_val(pp, 3.14);
    p = &y;
    set_val(pp, 2.718);
    print_double(x);
    print_double(y);
    return 0;
}
