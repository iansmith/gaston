// Test: function pointer typedef patterns from Lua
typedef long intptr_t;
typedef intptr_t lua_KContext;
struct lua_State;
struct lua_Debug;

typedef int (*lua_CFunction) (struct lua_State *L);
typedef int (*lua_KFunction) (struct lua_State *L, int status, lua_KContext ctx);
typedef const char * (*lua_Reader) (struct lua_State *L, void *ud, long sz);
typedef int (*lua_Writer) (struct lua_State *L, const void *p, long sz, void *ud);
typedef void * (*lua_Alloc) (void *ud, void *ptr, long osize, long nsize);
typedef void (*lua_WarnFunction) (void *ud, const char *msg, int tocont);
typedef void (*lua_Hook) (struct lua_State *L, struct lua_Debug *ar);

int main(void) {
    lua_CFunction cf = 0;
    lua_KFunction kf = 0;
    lua_Reader rd = 0;
    lua_Writer wr = 0;
    lua_Alloc al = 0;
    lua_WarnFunction wf = 0;
    lua_Hook hk = 0;
    (void)cf; (void)kf; (void)rd; (void)wr; (void)al; (void)wf; (void)hk;
    return 0;
}
