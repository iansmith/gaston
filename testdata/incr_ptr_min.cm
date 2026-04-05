/* minimal test to isolate segfault */
void main(void) {
    int arr[3];
    int* p;
    arr[0] = 0;
    arr[1] = 10;
    arr[2] = 20;
    p = arr;
    output(*p++);   /* should print 0 */
    output(*p);     /* should print 10 */
}
