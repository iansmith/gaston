// Test: parenthesized function names (Lua API pattern to prevent macro expansion)
// Plus: named struct inside union, struct-array typedef, const ptr-to-const array param

typedef long size_t;
typedef unsigned char lu_byte;
struct lua_State;
typedef void * (*lua_Alloc) (void *ud, void *ptr, long osize, long nsize);
typedef int (*lua_CFunction) (struct lua_State *L);
typedef double lua_Number;

// === Pattern 1: parenthesized function names ===
// Non-pointer return
extern void (lua_close) (struct lua_State *L);
extern int (lua_checkstack) (struct lua_State *L, int n);

// Pointer return
extern struct lua_State *(lua_newstate) (lua_Alloc f, void *ud);
extern const char *(lua_tolstring) (struct lua_State *L, int idx, size_t *len);
extern void *(lua_touserdata) (struct lua_State *L, int idx);

// Variadic, parenthesized name
extern const char *(lua_pushfstring) (struct lua_State *L, const char *fmt, ...);
extern void (lua_error_varg) (struct lua_State *L, const char *fmt, ...);

// === Pattern 2: const char *const lst[] parameter ===
extern int (luaL_checkoption) (struct lua_State *L, int arg, const char *def,
                               const char *const lst[]);

// === Pattern 3: named struct inside union (Lua's Node type) ===
typedef union {
    long d;
    void *p;
} Value;

typedef struct TValue {
    Value value_;
    lu_byte tt_;
} TValue;

typedef union Node {
    struct NodeKey {
        Value value_;
        lu_byte tt_;
        lu_byte key_tt;
        int next;
        Value key_val;
    } u;
    TValue i_val;
} Node;

// === Pattern 4: typedef struct as array (sigjmp_buf) ===
typedef long jmp_buf[16];
typedef unsigned long __sigset_t;

typedef struct {
    int savesigs;
    __sigset_t sigs;
    jmp_buf jmpb;
} sigjmp_buf[1];

// === Pattern 5: struct pointer array field and 2D pointer array field ===
typedef struct TString { int dummy; } TString;
struct Table;

typedef struct global_State {
    TString *tmname[24];
    struct Table *mt[9];
    TString *strcache[53][2];
} global_State;

int main(void) {
    Node n;
    n.u.key_tt = 0;
    n.i_val.tt_ = 1;
    (void)n;
    return 0;
}
