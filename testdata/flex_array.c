struct Buf {
    int len;
    int data[];
};

int sum_arr(int *arr, int n) {
    int i;
    int s;
    s = 0;
    for (i = 0; i < n; i = i + 1) {
        s = s + arr[i];
    }
    return s;
}

int main(void) {
    struct Buf *b;
    int *dp;
    b = malloc(40);
    b->len = 4;
    dp = b->data;
    dp[0] = 10;
    dp[1] = 20;
    dp[2] = 30;
    dp[3] = 40;
    output(b->len);
    output(sum_arr(dp, 4));
    output(sizeof(struct Buf));
    return 0;
}
