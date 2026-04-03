/* sort.cm — selection sort
   input:  10 integers
   output: sorted integers, then GCD of first two */

int arr[10];

int minIndex(int a[], int n, int start) {
    int min;
    int i;
    min = start;
    i = start + 1;
    while (i < n) {
        if (a[i] < a[min]) {
            min = i;
        }
        i = i + 1;
    }
    return min;
}

void swap(int a[], int i, int j) {
    int temp;
    temp = a[i];
    a[i] = a[j];
    a[j] = temp;
}

int gcd(int a, int b) {
    while (b != 0) {
        int t;
        t = b;
        b = a - (a / b) * b;
        a = t;
    }
    return a;
}

void selectionSort(int a[], int n) {
    int i;
    int m;
    i = 0;
    while (i < n - 1) {
        m = minIndex(a, n, i);
        if (m != i) {
            swap(a, i, m);
        }
        i = i + 1;
    }
}

void main(void) {
    int i;
    int n;
    int g;
    n = 10;
    i = 0;
    while (i < n) {
        arr[i] = input();
        i = i + 1;
    }
    selectionSort(arr, n);
    i = 0;
    while (i < n) {
        output(arr[i]);
        i = i + 1;
    }
    g = gcd(arr[0], arr[1]);
    output(g);
}
