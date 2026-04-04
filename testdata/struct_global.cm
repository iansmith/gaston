/* struct_global.cm — global struct variable */
struct Counter { int count; int total; };

struct Counter c;

void increment(int amount) {
    c.count = c.count + 1;
    c.total = c.total + amount;
}

void main(void) {
    c.count = 0;
    c.total = 0;
    increment(10);
    increment(20);
    increment(30);
    output(c.count);
    output(c.total);
}
