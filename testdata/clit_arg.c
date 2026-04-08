struct Dim { int w; int h; };

int area(struct Dim *d) {
    return d->w * d->h;
}

void main(void) {
    output(area(&(struct Dim){ .w = 9, .h = 11 }));
}
