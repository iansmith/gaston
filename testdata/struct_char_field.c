/* struct_char_field.cm — struct with char field followed by int field.
   LP64 layout: char@0 (1 byte), pad 3, int@4 (4 bytes, align 4), size=8. */
struct Mixed { char c; int i; };

void main(void) {
    struct Mixed m;
    m.c = 65;
    m.i = 42;
    output(m.c);
    output(m.i);
    output(sizeof(struct Mixed));
}
