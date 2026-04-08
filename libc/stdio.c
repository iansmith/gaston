/* stdio.cm — gaston low-level I/O helpers.
 *
 * These are the old test-helper functions (output, input, print_char,
 * print_string, print_double) reimplemented on top of real POSIX syscalls
 * and picolibc's printf.  They exist so that the ~200 existing test programs
 * continue to work without modification.
 *
 * Also provides __stdio_print_long used internally by other libc code.
 */

/* Forward declarations — provided by picolibc and our POSIX stubs */
extern long read(int fd, void *buf, long count);
extern long write(int fd, void *buf, long count);

/* ---- output(int) ---- print integer followed by newline to stdout ------- */

void output(int x) {
    char buf[24];
    char *p;
    int neg;
    long val;

    neg = 0;
    val = (long)x;
    if (val < 0) {
        neg = 1;
        val = 0 - val;
    }

    /* build digits right-to-left */
    p = buf + 23;
    *p = '\n';
    p = p - 1;

    if (val == 0) {
        *p = '0';
        p = p - 1;
    } else {
        while (val > 0) {
            *p = (char)(val % 10 + '0');
            p = p - 1;
            val = val / 10;
        }
    }
    if (neg) {
        *p = '-';
        p = p - 1;
    }
    p = p + 1;
    write(1, p, (long)(buf + 24 - p));
}

/* ---- input() ---- read one integer from stdin --------------------------- */

int input(void) {
    char buf[1];
    long val;
    int neg;
    long n;

    val = 0;
    neg = 0;

    /* skip whitespace */
    while (1) {
        n = read(0, buf, 1);
        if (n <= 0) { return 0; }
        if (buf[0] != ' ' && buf[0] != '\n' && buf[0] != '\r' && buf[0] != '\t') { break; }
    }

    /* sign */
    if (buf[0] == '-') {
        neg = 1;
        n = read(0, buf, 1);
        if (n <= 0) { return 0; }
    } else if (buf[0] == '+') {
        n = read(0, buf, 1);
        if (n <= 0) { return 0; }
    }

    /* digits */
    while (buf[0] >= '0' && buf[0] <= '9') {
        val = val * 10 + (buf[0] - '0');
        n = read(0, buf, 1);
        if (n <= 0) { break; }
    }

    if (neg) { val = 0 - val; }
    return (int)val;
}

/* ---- print_char(int) ---- write one character to stdout ----------------- */

void print_char(int c) {
    char buf[1];
    buf[0] = (char)c;
    write(1, buf, 1);
}

/* ---- print_string(char*) ---- write null-terminated string to stdout ---- */

void print_string(char *s) {
    int len;
    len = 0;
    while (s[len] != 0) {
        len = len + 1;
    }
    write(1, s, len);
}

/* ---- print_double(double) ---- print double with "%f\n" format ---------- */

/* Forward declaration — provided by printf.cm */
extern int printf(char *fmt, ...);

void print_double(double x) {
    printf("%f\n", x);
}

/* ---- __stdio_print_long ---- print long decimal without newline --------- */

void __stdio_print_pos(long n) {
    if (n >= 10) {
        __stdio_print_pos(n / 10);
    }
    print_char((int)(n % 10 + 48));
}

void __stdio_print_long(long n) {
    if (n < 0) {
        print_char('-');
        __stdio_print_pos(0 - n);
    } else {
        __stdio_print_pos(n);
    }
}
