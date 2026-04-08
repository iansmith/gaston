/* err_struct_ptr_alias.cm — two structs with identical field layouts but
   different tag names.  Their typedef'd pointer types must remain distinct:
   CatPtr != DogPtr even though both are "a pointer to a struct with one int".
   The type checker must compare struct tags, not struct contents. */

struct Cat { int weight; };
struct Dog { int weight; };

typedef struct Cat* CatPtr;
typedef struct Dog* DogPtr;

void main(void) {
    struct Cat c;
    CatPtr cp;
    DogPtr dp;

    c.weight = 5;
    cp = &c;
    dp = cp;   /* ERROR: CatPtr(=Cat*) != DogPtr(=Dog*) */
}
