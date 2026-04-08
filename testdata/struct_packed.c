/* struct_packed.c — __attribute__((packed)) struct layout.
   packed struct { char, int, char } should be 6 bytes (no padding).
   normal struct { char, int, char } should be 12 bytes (aligned). */

struct __attribute__((packed)) packed_before {
    char a;
    int  b;
    char c;
};

struct packed_after {
    char a;
    int  b;
    char c;
} __attribute__((packed));

struct normal {
    char a;
    int  b;
    char c;
};

typedef struct __attribute__((packed)) {
    char x;
    int  y;
} packed_typedef_t;

void main(void) {
    output(sizeof(struct packed_before)); /* 6 */
    output(sizeof(struct packed_after));  /* 6 */
    output(sizeof(struct normal));        /* 12 */
    output(sizeof(packed_typedef_t));     /* 5 */
}
