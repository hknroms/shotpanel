#include <stdio.h>
typedef struct lua_State lua_State;
lua_State *luaL_newstate(void);
void luaL_openlibs(lua_State *L);
int luaL_loadfilex(lua_State *L, const char *f, const char *mode);
int lua_pcallk(lua_State *L, int na, int nr, int ef, long ctx, void *k);
const char *lua_tolstring(lua_State *L, int i, size_t *len);
int main(int c, char **v){ lua_State *L=luaL_newstate(); luaL_openlibs(L);
 if(luaL_loadfilex(L,v[1],0)||lua_pcallk(L,0,0,0,0,0)){fprintf(stderr,"%s\n",lua_tolstring(L,-1,0));return 1;} return 0;}
