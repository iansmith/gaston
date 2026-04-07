package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// dockerTest describes one end-to-end test: compile a .cm file, run the
// resulting ARM64 ELF in an Alpine container, check stdout.
type dockerTest struct {
	name  string // base name (testdata/<name>.cm → /tmp/gaston-test-<name>)
	stdin string // bytes piped to the program's stdin (usually empty)
	want  string // expected exact stdout
}

var featureTests = []dockerTest{
	// ── Feature 1: print_char / print_string ─────────────────────────────
	{name: "pc_literal", want: "Hello\n"},
	{name: "pc_var", want: "ABCDE\n"},
	{name: "ps_basic", want: "hello\n"},
	{name: "ps_multi", want: "one\ntwo\nthree\n"},

	// ── Feature 2: multiple declarations ─────────────────────────────────
	{name: "multi_local", want: "30\n"},
	{name: "multi_global", want: "100\n200\n300\n"},
	{name: "multi_three", want: "12\n3\n15\n"},

	// ── Feature 3: const ─────────────────────────────────────────────────
	{name: "const_global", want: "100\n5\n95\n"},
	{name: "const_local", want: "10\n7\n70\n"},
	{name: "const_loop", want: "15\n"},
	{name: "const_expr", want: "1\n0\n20\n"},

	// ── Feature 4: char type and literals ────────────────────────────────
	{name: "char_literal", want: "Hi\n"},
	{name: "char_arith", want: "abcde\n"},
	{name: "char_var", want: "ABCDE\n"},
	{name: "str_basic", want: "hello world\nsecond line\n"},
	{name: "str_escape", want: "tab:\there\nslash: \\\n"},

	// ── Feature 5: pointers ──────────────────────────────────────────────
	{name: "ptr_basic", want: "42\n99\n"},
	{name: "ptr_param", want: "12\n"},
	{name: "ptr_swap", want: "3\n7\n"},
	{name: "ptr_array", want: "10\n20\n30\n"},
	{name: "ptr_global", want: "42\n"},

	// ── Feature 6: malloc/free ───────────────────────────────────────────
	{name: "malloc_basic", want: "0\n1\n4\n9\n16\n25\n36\n49\n64\n81\n"},
	{name: "malloc_local", want: "1\n9\n25\n"},
	{name: "malloc_two", want: "11\n22\n33\n44\n55\n"},
	{name: "malloc_func", want: "0\n2\n4\n6\n8\n10\n"},
	{name: "malloc_reuse", want: "1\n30\n"},
	{name: "malloc_large", want: "5050\n"},
	{name: "malloc_modify", want: "12\n22\n32\n"},

	// ── Feature 7: long / long long ──────────────────────────────────────
	{name: "long_basic", want: "3000000\n"},
	{name: "long_types", want: "1000000000\n2000000000\n3000000000\n4000000000\n3000000000\n"},

	// ── Feature 8: unsigned int / unsigned long ───────────────────────────
	// unsigned_div: 100/7=14, 100%7=2, UINT_MAX/2>0→1, UINT_MAX%2=1→1
	{name: "unsigned_div", want: "14\n2\n1\n1\n"},
	// unsigned_cmp: big(-1 unsigned) vs small(1): >, >=, <, <= all true; ==, != correct
	{name: "unsigned_cmp", want: "1\n1\n1\n1\n1\n1\n"},
	// unsigned_shr: (-8)>>62 = 3; (-4)>>1 > 0 unsigned → 1
	{name: "unsigned_shr", want: "3\n1\n"},
	// bitwise_unit: &,|,^,~,<<,>> on int; compound &=,|=,^=,<<=,>>=
	{name: "bitwise_unit", want: "0\n255\n255\n-241\n480\n120\n170\n85\n0\n255\n255\n0\n170\n51\n204\n10\n11\n9\n48\n3\n"},
	// bitwise_shift_sign: signed arithmetic >> vs unsigned logical >>; compound shifts
	{name: "bitwise_shift_sign", want: "-4\n-2\n-1\n-16\n2\n3\n2147483647\n128\n1\n128\n65536\n-1\n8\n2\n1073741824\n"},
	// bitwise_byzantine: mixed types, casts, XOR tricks, compound ops — see file for derivation
	// Note: (short)-256 ^ 65535 = -65281 because short promotes to 64-bit before XOR
	{name: "bitwise_byzantine", want: "0\n15\n-1\n-65281\n15\n205\n20\n10\n177\n181\n-128\n128\n-32768\n32\n15\n63\n5557743\n65280\n4080\n"},
	// unsigned_arith: 13,7,30,4, then compound: 15,12,24,6,2, then ul: 2000000000
	{name: "unsigned_arith", want: "13\n7\n30\n4\n15\n12\n24\n6\n2\n2000000000\n"},

	// ── Feature 9: short / unsigned short ────────────────────────────────
	{name: "short_basic", want: "3000\n3000\n"},
	// short_types: 1000, 20000, 100, compound: 150,50, unsigned short: 700
	{name: "short_types", want: "1000\n20000\n100\n150\n50\n700\n"},

	// ── Feature 10: float / double ───────────────────────────────────────
	// float_basic: literal, assignment, int conversion
	{name: "float_basic", want: "1\n3\n2\n1\n"},
	// float_arith: +, -, *, / with exact binary fractions
	{name: "float_arith", want: "5\n2\n6\n3\n3\n"},
	// float_cmp: <, <=, >, >=, ==, != operators
	{name: "float_cmp", want: "1\n1\n0\n0\n0\n1\n1\n0\n"},
	// float_neg: unary negation
	{name: "float_neg", want: "3\n1\n7\n"},
	// float_conv: double→int (truncation) and int→double
	{name: "float_conv", want: "7\n15\n3\n-2\n"},
	// float_global: global double variables
	{name: "float_global", want: "10\n5\n18\n8\n"},
	// float_loop: accumulation and multiplication in loops
	{name: "float_loop", want: "4\n16\n"},
	// float_if: FP comparisons controlling if/while
	{name: "float_if", want: "1\n0\n1\n3\n"},
	// float_func: double function parameters and return values
	{name: "float_func", want: "6\n2\n5\n4\n"},
	// float_print: print_double runtime (integer-valued and fractional doubles)
	{name: "float_print", want: "3.000000\n0.500000\n-1.250000\n100.000000\n"},

	// ── Feature: goto / labeled statements ──────────────────────────────
	// goto_basic: loop with goto, outputs 0–4 then 99
	{name: "goto_basic", want: "0\n1\n2\n3\n4\n99\n"},

	// ── Feature: variable-length arrays (VLAs) ───────────────────────────
	// vla_basic: int[n] with runtime n=5 → sum(0+2+4+6+8)=20; n=4 → sum(0+2+4+6)=12
	{name: "vla_basic", want: "20\n12\n"},
	// vla_param: VLA inside function; dot-product: 3→14 (1+4+9), 4→30 (1+4+9+16)
	{name: "vla_param", want: "14\n30\n"},

	// ── Integration ──────────────────────────────────────────────────────
	{name: "combo_all", want: "63\nABC\nok\n"},

	// ── Feature 11: structs ──────────────────────────────────────────────
	// struct_basic: local struct, assign and read fields
	{name: "struct_basic", want: "10\n20\n30\n"},
	// struct_short_field: struct with short field; LP64 layout (short@0, int@4, sizeof=8)
	{name: "struct_short_field", want: "1000\n42\n8\n32767\n9999\n"},
	// struct_float_field: struct with float field (item 1+2); float@0, int@4, sizeof=8
	{name: "struct_float_field", want: "3.500000\n100\n8\n9.750000\n100\n"},
	// struct_ptr: pointer to struct, -> access, pass to function
	{name: "struct_ptr", want: "3\n7\n10\n"},
	// struct_global: global struct variable, function modifies via . access
	{name: "struct_global", want: "3\n60\n"},
	// struct_nested: 4-field struct, larger offsets, pass by pointer to function
	{name: "struct_nested", want: "1\n2\n10\n20\n200\n"},
	// struct_char_field: char+int struct; LP64 layout (char@0, int@4, size=8)
	{name: "struct_char_field", want: "65\n42\n8\n"},

	// ── Feature 12: variadic functions ───────────────────────────────────
	// variadic_basic: variadic sum of N integer args
	{name: "variadic_basic", want: "60\n100\n10\n"},
	// variadic_ptr: variadic function reading string pointer args
	{name: "variadic_ptr", want: "hello\nworld\ndone\n"},
	// vadouble_basic: va_arg with double — reads double bits and truncates to long
	{name: "vadouble_basic", want: "3\n7\n100\n"},

	// ── Feature 13: pointer arithmetic / void* / double pointers ─────────
	// ptr_double: store and load double values via double* pointer
	{name: "ptr_double", want: "3.000000\n7.000000\n7.000000\n"},
	// ptr_float: store and load float values via float* pointer (item 1: TypeFloatPtr)
	{name: "ptr_float", want: "2.500000\n7.250000\n7.250000\n"},
	// ptr_arith: p+n auto-scales by 8; p-1 retreats one element
	{name: "ptr_arith", want: "10\n20\n30\n40\n30\n20\n"},
	// ptr_inc: p++/p--/p+=/p-= advance/retreat by element size
	{name: "ptr_inc", want: "10\n20\n40\n30\n10\n"},
	// ptr_void: void* accepts int* in assignment; malloc returns void*
	{name: "ptr_void", want: "0\n2\n4\n"},
	// ptr_ptr: int** double pointer dereference and assignment through
	{name: "ptr_ptr", want: "42\n99\n99\n"},
	// ptr_ptr_arr: int** subscript on malloc'd array of pointers
	{name: "ptr_ptr_arr", want: "10\n20\n30\n20\n"},
	// char_ptr_ptr: char** array of string pointers
	{name: "char_ptr_ptr", want: "alpha\nbeta\ngamma\n"},

	// ── Feature 14: pointer comparisons ──────────────────────────────────
	// ptr_cmp: null check and same-type ordering comparisons
	{name: "ptr_cmp", want: "0\n1\n0\n1\n"},

	// ── Feature 15: sizeof operator ──────────────────────────────────────
	// sizeof_basic: sizeof(type), sizeof(expr), sizeof(struct) — LP64: int=4, ptr=8
	{name: "sizeof_basic", want: "4\n1\n4\n1\n8\n8\n8\n"},
	// struct_int_layout: verifies sizeof(int)==4 and LP64 struct layouts with int fields
	{name: "struct_int_layout", want: "8\n8\n16\n4\n7\n42\n1\n"},
	// sizeof_array: sizeof(local_arr)=N×8, sizeof(arr_param)=8, sizeof(global_arr)=N×8
	{name: "sizeof_array", want: "24\n40\n8\n"},
	// sizeof_types: sizeof for float=4, double=8, short=2, unsigned short=2, unsigned char=1, int=4, char=1
	{name: "sizeof_types", want: "4\n8\n2\n2\n1\n4\n1\n"},

	// ── Feature 16: struct-by-value fields ───────────────────────────────
	// struct_value: nested struct fields; LP64 layout: Point=8, Rect=16
	{name: "struct_value", want: "1\n2\n10\n20\n12\n8\n16\n"},

	// ── Feature 17: pointer assignment type checking ──────────────────────
	// ptr_compat: void*↔any pointer, same-type, and null constant are all valid
	{name: "ptr_compat", want: "99\n99\n"},

	// ── Feature 18: integer promotion (char/short → int before arithmetic) ─
	// int_promote: signed/unsigned char and short overflow, compound assign
	{name: "int_promote", want: "-128\n-56\n-55\n200\n0\n-32768\n0\n"},
	// int_promote_arith: cross-type char+short arithmetic; no intermediate overflow
	{name: "int_promote_arith", want: "254\n-2\n30000\n30000\n0\n"},

	// ── Item 9: enum / union / typedef / function pointers / const* ──────────
	// enum_basic: enum constants auto-increment from 0; explicit value restarts counter
	{name: "enum_basic", want: "0\n1\n2\n10\n11\n12\n"},
	// union_basic: all fields share offset 0; LP64: sizeof = max(sizeof(int)=4, sizeof(char)=1) = 4
	{name: "union_basic", want: "1094861636\n65\n4\n"},
	// typedef_basic: typedef creates an alias; variables declared with typedef'd name
	{name: "typedef_basic", want: "42\n50\n"},
	// funcptr_basic: function pointer assign and call
	{name: "funcptr_basic", want: "7\n12\n30\n"},
	// funcptr_struct: function pointer called through a struct field
	{name: "funcptr_struct", want: "42\n63\n"},

	// ── Integration tests: features used in combination ───────────────────
	// deep_struct: 5-level nested struct; chained dot access; sizeof at each level (LP64: L5=4)
	{name: "deep_struct", want: "1\n2\n3\n4\n5\n4\n16\n24\n32\n40\n"},
	// union_in_struct: struct↔union alternating nesting; aliasing; sizeof (LP64: Point=8, Shape=16)
	{name: "union_in_struct", want: "7\n3\n4\n3\n8\n8\n16\n16\n"},
	// enum_flags: enum bit flags combined with bitwise ops
	{name: "enum_flags", want: "5\n1\n0\n4\n7\n6\n2\n1\n"},
	// typedef_funcptr_param: typedef'd func ptr as local, global, and function parameter
	{name: "typedef_funcptr_param", want: "7\n12\n15\n5\n25\n"},
	// const_ptr_alias: const* aliasing; modify via writable alias; re-point const*
	{name: "const_ptr_alias", want: "42\n99\n99\n100\n10\n30\n"},
	// enum_union_dispatch: all 5 new features together — tagged union with enum discriminant,
	// typedef'd func ptr as callback parameter
	{name: "enum_union_dispatch", want: "42\n3.140000\n7\n99\n"},

	// ── Byzantine stress tests (one per type-system gap item) ─────────────────────
	// Item 2: struct with char+short+int+double+ptr; LP64 layout, sizeof=24
	{name: "struct_mixed_fields", want: "24\n65\n1000\n999999\n2.500000\n77\n"},
	// Item 3: 3-level mixed -> . . chains; double field via IRFFieldLoad at depth 3
	{name: "deep_arrow_dot", want: "42\n1.500000\n99\n7\n16\n24\n32\n"},
	// Item 6: sizeof used in arithmetic, as divisor for element count, in comparisons
	{name: "sizeof_exprs", want: "40\n4\n5\n15\n1\n0\n"},
	// Item 7: void* round-trip through three functions; int* and double* round-trips
	{name: "void_ptr_chain", want: "42\n99\n7.000000\n"},
	// Item 8: char+char → int (no overflow); stored back to char (wraps); short overflow
	{name: "promo_wrap", want: "200\n-56\n10000\n-32768\n"},
	// Item 9a: function pointer as struct field; "vtable" dispatch through pointer
	{name: "vtable", want: "3\n8\n15\n0\n"},
	// Item 9b: enum constants as array indices, in arithmetic, mixed with sizeof
	{name: "enum_arith", want: "3\n1\n0\n30\n97\n3\n"},
	// Item 9c: const* aliasing, re-seat, passed to function
	{name: "const_ptr_write", want: "10\n20\n20\n30\n"},
	// Item 13: VLA filled in loop, passed to function; two different sizes
	{name: "vla_sum", want: "55\n15\n4\n"},
	// Items 1+4: double* pointer arithmetic, *(p+k) reads at stride 8
	{name: "double_ptr_ops", want: "1.000000\n3.000000\n6.000000\n9.000000\n"},
	// Items 4+12: char** as pointer table; deref through double indirection
	{name: "charpp_table", want: "65\n66\n90\n"},
	// Items 2+3+9 combined: union with double inside nested struct, 3-level dot chain
	{name: "union_float_chain", want: "42\n3.140000\n42\n24\n"},
	// Item 5: sizeof(int)=4 (LP64 C ABI); sizeof(long)=8; struct{int,int}=8
	{name: "sizeof_int_abi", want: "4\n8\n8\n"},

	// ── Item 4: double** and float** pointer-to-pointer types ─────────────
	{name: "dbl_ptr_ptr", want: "3.140000\n2.718000\n"},
	// ── Item 11: 2D arrays ────────────────────────────────────────────────
	{name: "multi_dim", want: "66\n0\n11\n"},
	// ── Item 12: arrays of pointers ───────────────────────────────────────
	{name: "ptr_arr", want: "10\n20\n30\n99\n"},
	// ── Item 14: _Bool / bool type ────────────────────────────────────────
	{name: "bool_basic", want: "1\n0\n1\n1\n"},
	// ── Item 15: bit-fields ───────────────────────────────────────────────
	{name: "bitfield_basic", want: "5\n17\n100\n8\n"},
	// ── Item 16: flexible array members ──────────────────────────────────
	{name: "flex_array", want: "4\n100\n4\n"},
	// ── Item 17: static local variables ──────────────────────────────────
	{name: "static_local", want: "1\n2\n3\n"},
	// ── Items 17+18: register/volatile as no-ops ─────────────────────────
	{name: "register_volatile", want: "42\n43\n"},

	// ── va_list / va_arg built-in ─────────────────────────────────────────
	// va_arg_basic: va_arg reads int, double, and char* args, advancing the
	// va_list pointer correctly for each type.
	{name: "va_arg_basic", want: "60\n10\n4.000000\n6.000000\nalpha\ngamma\n"},

	// ── Parameterised TypePtr / CType byzantine pointer tests ─────────────
	// ptr_triple_chain: int*** via three-level typedef chain; read and write
	// through all three levels of indirection.
	{name: "ptr_triple_chain", want: "10\n30\n30\n20\n77\n"},
	// typedef_ptr_layers: struct Point* via PointPtr/PointPtrPtr typedefs;
	// function parameter, double-deref, arrow-through-deref, pointer redirect.
	{name: "typedef_ptr_layers", want: "7\n7\n300\n300\n15\n3\n3\n"},
	// void_ptr_launder: void* used as universal adapter at each level;
	// int*→void*→int* and int**→void*→int** round-trips must be transparent.
	{name: "void_ptr_launder", want: "42\n42\n99\n99\n"},

	// ── x++/x--/++x/--x ──────────────────────────────────────────────────
	// incr_stmt: all four forms used as statements (return value discarded)
	{name: "incr_stmt", want: "6\n5\n6\n5\n"},
	// incr_expr: postfix returns old value; prefix returns new value
	{name: "incr_expr", want: "10\n11\n11\n10\n11\n11\n10\n10\n4\n4\n6\n5\n"},
	// incr_ptr: pointer increment/decrement scales by element size; expression forms
	{name: "incr_ptr", want: "0\n10\n20\n10\n0\n0\n10\n20\n20\n"},
	// incr_arr: postfix/prefix on array elements and through pointer deref (++*p)
	{name: "incr_arr", want: "10\n11\n21\n21\n31\n31\n30\n30\n"},
	// incr_field: postfix/prefix on struct fields via dot and arrow
	{name: "incr_field", want: "5\n6\n7\n7\n7\n6\n5\n5\n"},

	// ── PRINTF-FEATURES item 4: type casts ───────────────────────────────
	// cast_int_double: (int)3.14→3; (int)((double)5)→5
	{name: "cast_int_double", want: "3\n5\n"},
	// cast_char: (char)300 wraps to 44 (300 & 0xFF)
	{name: "cast_char", want: "44\n"},
	// cast_ptr: (int*)0 is null; null == 0 is true
	{name: "cast_ptr", want: "1\n"},
	// cast_unsigned: (unsigned)(-1) is max unsigned; > 0 is true
	{name: "cast_unsigned", want: "1\n"},
	// assign_cross_width: narrowing (int→short/char, short→char) and widening
	//   (char/short/uchar/ushort → int/long/short) assignments; truncation via load-time sign/zero ext
	{name: "assign_cross_width", want: "-31072\n34464\n-96\n4464\n12\n-100\n-100\n-100\n200\n200\n-500\n60000\n"},
	// cast_cross_width: (short), (unsigned short), (char), (unsigned char) casts from int/-1/etc.,
	//   plus multi-step chains: (int)(short)N, (char)(short)N, (short)(char)N, (us)(char)(-100)
	{name: "cast_cross_width", want: "-31072\n34464\n-96\n160\n-1\n65535\n255\n-31072\n-96\n44\n34464\n200\n65436\n"},

	// ── PRINTF-FEATURES item 5: struct return by value ────────────────────
	// struct_ret_small: 1-field struct (≤8 bytes) returned in X0
	{name: "struct_ret_small", want: "42\n"},
	// struct_ret_medium: 2-field int struct; LP64 size=8 (≤8 bytes) returned in X0
	{name: "struct_ret_medium", want: "10\n20\n"},
	// struct_ret_large: 3-field struct (>16 bytes) returned via hidden X8 pointer
	{name: "struct_ret_large", want: "7\n8\n9\n"},
	// struct_copy: x = y struct assignment (IRStructCopy word-copy loop)
	{name: "struct_copy", want: "11\n22\n22\n11\n1\n2\n3\n4\n"},
	// nested_struct_3level: 3-level nesting (Leaf→Mid→Top), chained field write+read
	{name: "nested_struct_3level", want: "10\n20\n99\n"},
	// nested_struct_decompose: Rect with two Vec2, decompose fields, compute area
	{name: "nested_struct_decompose", want: "2\n3\n7\n8\n25\n"},
	// nested_struct_chain_call: segments/midpoints built via chained calls and field access
	{name: "nested_struct_chain_call", want: "5\n2\n5\n2\n10\n4\n"},
	// nested_struct_assign: x=outer.inner and outer.inner=x (struct-typed field as lvalue/rvalue)
	{name: "nested_struct_assign", want: "1\n2\n9\n8\n9\n8\n30\n40\n"},

	// ── Struct-by-value parameters (all three ABI paths) ─────────────────
	// struct_param_small: ≤8-byte struct param (1 field), read field in callee
	{name: "struct_param_small", want: "42\n"},
	// struct_param_medium: ≤16-byte struct param (2 fields, X0+X1), both fields summed
	{name: "struct_param_medium", want: "30\n"},
	// struct_param_large: >16-byte struct param (3 fields, pointer), all fields summed
	{name: "struct_param_large", want: "6\n"},
	// struct_param_mixed: f(int, Pair, int) — interleaved register consumption
	{name: "struct_param_mixed", want: "10\n"},
	// struct_param_isolate: callee mutates copy; caller value unchanged (≤16)
	{name: "struct_param_isolate", want: "7\n"},
	// struct_param_ret_combo: struct param + struct return in same function (swap)
	{name: "struct_param_ret_combo", want: "9\n5\n"},
	// struct_param_multi: two struct params (dist2(Point a, Point b))
	{name: "struct_param_multi", want: "25\n"},
	// struct_param_field_arg: f(outer.inner) — exercises allocStructSlot + IRStructCopy path
	{name: "struct_param_field_arg", want: "42\n"},
	// struct_by_value_integration: all three size classes + return + assign in one program
	{name: "struct_by_value_integration", want: "10\n8\n12\n6\n12\n3\n3\n2\n4\n8\n4\n5\n13\n3\n2\n0\n"},
	// struct_byval_byzantine: adversarial combinations — medium param+return (Mat2=16) live together,
	// chained call-result-as-param, mixed register classes, double-transpose,
	// nested field assign then pass as param, recursion, two medium params+return.
	{name: "struct_byval_byzantine", want: "1\n3\n2\n4\n8\n12\n6\n12\n8\n16\n1\n2\n3\n4\n25\n15\n6\n7\n7\n8\n"},

	// ── Tier 3 item 11: anonymous struct/union members ───────────────────
	// anon_struct_basic: anonymous struct inside struct; fields accessed directly
	// on outer; LP64 layout: a@0, anon@8(b@8,c@12), d@16; sizeof=24
	{name: "anon_struct_basic", want: "1\n2\n3\n4\n24\n"},
	// anon_union_basic: anonymous union inside struct; aliasing via overlapping
	// fields; tag unaffected; sizeof=16
	{name: "anon_union_basic", want: "7\n1000\n65\n833\n16\n"},
	// anon_multi: two anonymous structs in one struct; -> access through pointer;
	// LP64 layout: anon0@0(x@0,y@4), anon1@8(z@8,w@12); sizeof=16
	{name: "anon_multi", want: "10\n20\n30\n40\n10\n30\n16\n"},

	// ── Tier 3 item 12: adjacent string literal concatenation ─────────────
	// adj_str_basic: 2-part, 3-part, empty-prefix, empty-middle concat
	{name: "adj_str_basic", want: "Hello, World!\nfoobarbaz\nnonempty\nAB\n"},
	// adj_str_escape: concat across escape sequences (\n, \t, \")
	{name: "adj_str_escape", want: "line1\nline2\ncol1\tcol2\tcol3\nsay \"hi\"\n"},

	// ── Tier 3 item 13: _Noreturn / GCC qualifier keywords ───────────────
	// noreturn_qual: _Noreturn, __inline__, __restrict__, __signed__ silently
	// accepted and dropped; underlying code behaves identically
	{name: "noreturn_qual", want: "42\n21\n50\n-99\n"},

	// ── Tier 3 item 15: wchar_t pre-registered typedef ───────────────────
	// wchar_t_basic: wchar_t scalar, array, arithmetic, unsigned semantics;
	// sizeof(unsigned int)=4; no sign extension at 200
	{name: "wchar_t_basic", want: "65\n4\n100\n200\n300\n400\n75\n200\n"},

	// ── Designated initializers (C99 §6.7.8) ─────────────────────────────
	// desinit_struct: .field designators on 3-field struct, out-of-order
	{name: "desinit_struct", want: "10\n20\n30\n"},
	// desinit_partial: only some fields designated; unspecified fields zero
	{name: "desinit_partial", want: "5\n0\n99\n"},
	// desinit_array: [index] designators; sparse array; zeros for unset slots
	{name: "desinit_array", want: "1\n0\n0\n4\n9\n"},
	// desinit_mixed: mix of plain and designated entries; position tracking
	{name: "desinit_mixed", want: "0\n10\n20\n0\n"},
	// desinit_nested: struct with a struct field init using nested braces
	{name: "desinit_nested", want: "10\n1\n2\n99\n"},
	// desinit_union: single designator on a union; aliased value
	{name: "desinit_union", want: "42\n42\n"},
	// desinit_global: global struct with constant designator initializers
	{name: "desinit_global", want: "1\n2\n3\n"},

	// ── Torture test: combined features 10+11+12+14 ──────────────────────
	{name: "torture_mix", want: "30\n40\n3\n4\n0\n24\n10\n20\n100\n200\n0\n330\n2500\n116\n116\n101\n65\n66\n67\n7\n42\n42\n42\n10\n20\n30\n15\n"},

	// ── Compound literals (C99 §6.5.2.5) ─────────────────────────────────
	// clit_struct: (struct T){...} passed by pointer; fields read back
	{name: "clit_struct", want: "1\n2\n"},
	// clit_array: (int[]){...} — pointer to first element; subscript read
	{name: "clit_array", want: "10\n20\n30\n"},
	// clit_scalar: (int){42} — scalar compound literal used as rvalue
	{name: "clit_scalar", want: "42\n"},
	// clit_nested: compound literal containing a designated nested struct field
	{name: "clit_nested", want: "7\n8\n"},
	// clit_arg: compound literal passed directly as a function argument
	{name: "clit_arg", want: "99\n"},

	// ── PRINTF-FEATURES items 1–3 ─────────────────────────────────────────
	// static/inline on functions: static and inline qualifiers parse and compile
	{name: "static_inline_func", want: "6\n16\n"},
	// bool keyword as typedef alias: typedef _Bool bool; works without re-lexing bool as INT
	{name: "bool_typedef", want: "1\n0\n"},
	// logical &&: short-circuit, 0/1 result, use in conditions
	{name: "logical_and", want: "1\n1\n0\n1\n"},
	// logical ||: short-circuit, 0/1 result, use in conditions
	{name: "logical_or", want: "1\n0\n1\n1\n1\n"},
	// ternary_basic: sign/abs/max2 via nested ternary expressions
	{name: "ternary_basic", want: "-1\n0\n1\n7\n4\n8\n10\n"},
	// ternary_fp: double abs and max via ternary (FP branch selection)
	{name: "ternary_fp", want: "3.140000\n2.710000\n2.500000\n"},
	// hexfloat_basic: C99 hex float literals (0x<hex>p<exp>)
	{name: "hexfloat_basic", want: "1.000000\n3.000000\n0.500000\n1024.000000\n1.937500\n"},

	// ── GCC bit-manipulation intrinsics ──────────────────────────────────
	// builtin_bitops: __builtin_clz, __builtin_ctz, __builtin_popcount (32-bit variants)
	{name: "builtin_bitops", want: "31\n0\n15\n0\n3\n4\n0\n1\n8\n32\n16\n"},

	// ── typeof / __typeof__ GCC extension ────────────────────────────────
	// typeof_basic: typeof(var), typeof(type), typeof(expr) in declarations
	{name: "typeof_basic", want: "43\n6.280000\n100\n85\n"},

	// ── Statement expressions (GCC extension, Tier 4 item 17) ────────────
	{name: "stmtexpr_basic", want: "42\n"},
	{name: "stmtexpr_decl", want: "84\n"},
	{name: "stmtexpr_typeof", want: "3\n7\n"},
	{name: "stmtexpr_fp", want: "3.140000\n"},
	{name: "stmtexpr_nested", want: "10\n"},

	// ── __int128 / __uint128_t (Tier 4 item 19) ──────────────────────────
	// int128_basic: __int128 declaration, assignment, cast to/from long
	{name: "int128_basic", want: "42\n"},
	// int128_mul: (__uint128_t)a * b >> 64 — high word of 2^63 * 2 = 2^64
	{name: "int128_mul", want: "1\n"},
	// int128_arith: equality, less-than, addition
	{name: "int128_arith", want: "1\n0\n1\n"},
	// int128_cast: cast chain long → __uint128_t → long
	{name: "int128_cast", want: "100\n"},
	// inline_union_local: local variable with inline anonymous union type + brace-init
	{name: "inline_union_local", want: "1\n"},
	// paren_assign: parenthesized lvalue "(x) = expr"
	{name: "paren_assign", want: "43\n"},
}

// sepTest describes a separate-compilation test: compile multiple .cm files
// to .o, link them, run the result in Docker.
type sepTest struct {
	name  string   // test name (for /tmp paths and expected-output file)
	files []string // testdata/<file>.cm sources (first must contain main)
	want  string   // expected exact stdout
}

var sepTests = []sepTest{
	// two files: extern functions (add, mul)
	{name: "sep_basic", files: []string{"sep_main", "sep_lib"}, want: "7\n12\n"},
	// extern global variable shared between files
	{name: "sep_globals", files: []string{"sep_globals_main", "sep_globals_lib"}, want: "3\n"},
	// three-file call chain: main → compute → double_it
	{name: "sep_chain", files: []string{"sep_chain_main", "sep_chain_a", "sep_chain_b"}, want: "11\n"},
	// mutual recursion across two files (is_odd ↔ is_even)
	{name: "sep_mutual", files: []string{"sep_mutual_a", "sep_mutual_b"}, want: "1\n0\n"},
	// pointer parameter crosses file boundary; malloc used in main file
	{name: "sep_ptr", files: []string{"sep_ptr_main", "sep_ptr_lib"}, want: "2\n4\n6\n8\n10\n"},
}

// compileObj compiles testdata/<name>.cm to an ET_REL object at outPath.
func compileObj(name, outPath string) error {
	srcPath := fmt.Sprintf("testdata/%s.cm", name)
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	pp := newPreprocessor(nil, nil)
	src, err := pp.Preprocess(string(raw), srcPath)
	if err != nil {
		return fmt.Errorf("preprocess %s: %w", srcPath, err)
	}
	lex := newLexer(src, srcPath)
	yyParse(lex)
	if lex.errors > 0 {
		return fmt.Errorf("%s: %d parse error(s)", name, lex.errors)
	}
	if lex.result == nil {
		return fmt.Errorf("%s: empty program", name)
	}
	// requireMain=false: library files don't need main
	if err := semCheck(lex.result, false); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	irp := genIR(lex.result)
	return genObjectFile(irp, outPath)
}

// TestSepCompile compiles multiple .cm files to .o, links them, and runs the
// result in an Alpine ARM64 container.
func TestSepCompile(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}

	for _, tt := range sepTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var objPaths []string
			for _, f := range tt.files {
				obj := fmt.Sprintf("/tmp/gaston-test-%s-%s.o", tt.name, f)
				objPaths = append(objPaths, obj)
				t.Cleanup(func() { os.Remove(obj) })
				if err := compileObj(f, obj); err != nil {
					t.Fatalf("compile %s: %v", f, err)
				}
			}

			binPath := fmt.Sprintf("/tmp/gaston-test-%s", tt.name)
			t.Cleanup(func() { os.Remove(binPath) })
			if err := link(binPath, objPaths); err != nil {
				t.Fatalf("link: %v", err)
			}

			cmd := exec.Command("docker", "run", "--rm",
				"--platform", "linux/arm64",
				"-i",
				"-v", binPath+":/prog",
				"alpine:latest",
				"/prog",
			)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("docker run failed (exit %d):\nstdout: %s\nstderr: %s",
						ee.ExitCode(), string(out), string(ee.Stderr))
				}
				t.Fatalf("docker run: %v", err)
			}

			got := string(out)
			if got != tt.want {
				t.Errorf("output mismatch:\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}

// compileTest compiles testdata/<name>.cm to an ARM64 ELF at outPath using
// gaston's internal pipeline (no subprocess).
func compileTest(name, outPath string) error {
	srcPath := fmt.Sprintf("testdata/%s.cm", name)
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	pp := newPreprocessor(nil, nil)
	src, err := pp.Preprocess(string(raw), srcPath)
	if err != nil {
		return fmt.Errorf("preprocess %s: %w", srcPath, err)
	}
	lex := newLexer(src, srcPath)
	yyParse(lex)
	if lex.errors > 0 {
		return fmt.Errorf("%s: %d parse error(s)", name, lex.errors)
	}
	if lex.result == nil {
		return fmt.Errorf("%s: empty program", name)
	}
	if err := semCheck(lex.result, true); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	irp := genIR(lex.result)
	return genELF(irp, outPath)
}

// compileObjPath compiles a .cm file at srcPath to an ET_REL object at outPath,
// using the given include search paths.
func compileObjPath(srcPath, outPath string, includePaths []string, defines ...string) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	pp := newPreprocessor(includePaths, defines)
	src, err := pp.Preprocess(string(raw), srcPath)
	if err != nil {
		return fmt.Errorf("preprocess %s: %w", srcPath, err)
	}
	lex := newLexer(src, srcPath)
	yyParse(lex)
	if lex.errors > 0 {
		return fmt.Errorf("%s: %d parse error(s)", srcPath, lex.errors)
	}
	if lex.result == nil {
		return fmt.Errorf("%s: empty program", srcPath)
	}
	if err := semCheck(lex.result, false); err != nil {
		return fmt.Errorf("%s: %w", srcPath, err)
	}
	irp := genIR(lex.result)
	return genObjectFile(irp, outPath)
}

// libcTest describes a test that links against the gaston libc (libc/stdio.cm).
type libcTest struct {
	name string // testdata/<name>.cm is the main program
	want string // expected stdout
}

var libcTests = []libcTest{
	// ── Feature 13: libc printf / puts / putchar ──────────────────────────
	{name: "hello_world", want: "Hello, world!\n"},
	{name: "printf_simple", want: "42\n3.140000\n"},
	{name: "printf_fmt",  want: "count=42\nstr=hello!\nchar=A\n3+4=7\n"},
	{name: "puts_test",   want: "one\ntwo\nthree\n"},
	// ── Feature 14: libc sscanf ───────────────────────────────────────────
	{name: "sscanf_basic", want: "n=42 r=1\ns=hello r=1\na=-7 b=99 r=2\nc=X r=1\n"},
	// ── Feature 15: libc printf float ────────────────────────────────────
	{name: "printf_float",   want: "3.140000\n2.72\n1.234568e+04\n0.000123\n123456\n"},
	{name: "printf_f_large",  want: "1234567890.000000\n9876543210.500000\n-12345678901.000000\n1000000000.13\n"},
	// ── Feature 16: sprintf / snprintf ───────────────────────────────────
		// snprintf returns would-be-written count per C standard (not actual written count).
	{name: "snprintf_basic",  want: "x=42\npi=3.141590\nhello world len=11\nhello len=11\nempty='' len=3\n"},
	// ── Feature 17: sscanf float precision + inf/nan (P1-A, P1-B) ─────────
	{name: "sscanf_fp",      want: "n=1 v=3.14159265\nn=1 v=0.001234\nn=1 v=-2.71828\nn=1 v=150\nn=1 v=0.75\nn=1 v=inf\nn=1 v=-inf\nn=1 v=nan\nn=1 v=nan\n"},
	// ── Feature 18: sscanf scanset %[...] (P1-C) ─────────────────────────
	{name: "sscanf_scanset", want: "n=1 s=hello\nn=1 s=hello\nn=1 s=12345\nn=1 s=abc123\nn=2 a=hello b=world\nn=1 s=rest\n"},
	// ── Feature 19: printf %p pointer format (P2-B) ───────────────────────
	{name: "printf_ptr",     want: "0x000000000000002a\n0x0000000000000000\n0x00000000deadbeef\n"},
	// ── Feature 20: sscanf signed overflow clamping (P2-C) ───────────────
	{name: "sscanf_overflow", want: "n=1 v=9223372036854775807\nn=1 v=-9223372036854775808\nn=1 v=9223372036854775807\nn=1 v=9223372036854775807\n"},
	// ── Feature 21: sscanf EOF return (P2-D) ─────────────────────────────
	{name: "sscanf_eof",     want: "n=-1\nn=-1\nn=1 v=42\nn=0\n"},
	// vadouble_libc: va_arg double in object-file mode
	{name: "vadouble_libc", want: "3 7 100\n"},
}

// buildLibgastonc compiles every .cm file under libc/ plus picolibc tinystdio
// sources to object files, archives them all into libgastonc.a, and returns
// the archive path.  The caller must clean up.
func buildLibgastonc(t *testing.T) (libPath string) {
	t.Helper()
	libPath = "/tmp/gaston-test-libgastonc.a"
	t.Cleanup(func() { os.Remove(libPath) })

	var objPaths []string

	// ── gaston .cm sources (libc/) ──────────────────────────────────────
	entries, err := os.ReadDir("libc")
	if err != nil {
		t.Fatalf("read libc dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".cm") {
			continue
		}
		src := "libc/" + e.Name()
		obj := fmt.Sprintf("/tmp/gaston-test-libgastonc-%s.o",
			strings.TrimSuffix(e.Name(), ".cm"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, nil); err != nil {
			t.Fatalf("compile %s: %v", src, err)
		}
		objPaths = append(objPaths, obj)
	}

	// ── picolibc tinystdio sources ──────────────────────────────────────
	tsdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/tinystdio"
	tsInc := tinystdioIncludePaths()
	tsDefines := []string{"__PICOLIBC__=1", "TINY_STDIO=1", "FORMAT_DEFAULT_DOUBLE=1"}
	for _, f := range []string{"vfprintf.c", "filestrput.c", "dtoa_engine.c", "dtoa_data.c"} {
		src := tsdir + "/" + f
		obj := fmt.Sprintf("/tmp/gaston-test-libgastonc-ts-%s.o", strings.TrimSuffix(f, ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, tsInc, tsDefines...); err != nil {
			t.Fatalf("compile tinystdio %s: %v", f, err)
		}
		objPaths = append(objPaths, obj)
	}

	// ── picolibc string functions ───────────────────────────────────────
	stringDir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/string"
	strInc := stringIncludePaths()
	strDefines := []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"}
	for _, f := range []string{"strcmp.c", "strlen.c", "strnlen.c", "memset.c", "memcpy.c"} {
		src := stringDir + "/" + f
		obj := fmt.Sprintf("/tmp/gaston-test-libgastonc-str-%s.o", strings.TrimSuffix(f, ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, strInc, strDefines...); err != nil {
			t.Fatalf("compile string %s: %v", f, err)
		}
		objPaths = append(objPaths, obj)
	}

	if len(objPaths) == 0 {
		t.Fatal("no source files compiled for libgastonc")
	}
	if err := archiveCreate(libPath, objPaths); err != nil {
		t.Fatalf("archive libgastonc.a: %v", err)
	}
	return libPath
}

// libmIncludePaths returns the include search paths for picolibc libm sources.
// Paths are relative to the cmd/gaston directory (where tests run).
func libmIncludePaths() []string {
	return []string{
		"libm/common",  // fdlibm.h, math_config.h
		"libm/include", // math.h, errno.h, picolibc.h, sys/*, machine/*
	}
}

// TestLibmCompile compiles every picolibc libm/math/*.c source file and
// archives them into libm.a.  This verifies gaston can parse and codegen all
// 87 libm math sources without needing Docker or QEMU.
func TestLibmCompile(t *testing.T) {
	entries, err := os.ReadDir("libm/math")
	if err != nil {
		t.Fatalf("read libm/math: %v", err)
	}
	includePaths := libmIncludePaths()

	var objPaths []string
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		src := "libm/math/" + e.Name()
		obj := fmt.Sprintf("/tmp/libm-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths); err != nil {
			t.Errorf("compile %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		objPaths = append(objPaths, obj)
	}
	if len(failed) > 0 {
		t.Fatalf("%d/%d files failed: %v", len(failed), len(failed)+len(objPaths), failed)
	}
	if len(objPaths) == 0 {
		t.Fatal("libm/math contains no .c files")
	}

	libPath := "/tmp/libm.a"
	t.Cleanup(func() { os.Remove(libPath) })
	if err := archiveCreate(libPath, objPaths); err != nil {
		t.Fatalf("archive libm.a: %v", err)
	}
	t.Logf("built libm.a from %d objects", len(objPaths))
}

// TestDiagPreprocess is a helper to preprocess a file and show lines around an error.
// Not normally run — set the file names in the test to use.
func TestDiagPreprocess(t *testing.T) {
	t.Skip("diagnostic only — enable manually")
	files := []struct {
		src  string
		line int
	}{
		{"libm/common/s_fma.c", 17015},
		{"libm/common/sf_fma.c", 16970},
		{"libm/common/s_llrint.c", 16860},
		{"libm/common/s_llround.c", 16839},
		{"libm/common/sf_llrint.c", 16838},
		{"libm/common/sf_llround.c", 16809},
	}
	includePaths := libmIncludePaths()
	for _, f := range files {
		raw, err := os.ReadFile(f.src)
		if err != nil {
			t.Logf("read %s: %v", f.src, err)
			continue
		}
		pp := newPreprocessor(includePaths, nil)
		src, ppErr := pp.Preprocess(string(raw), f.src)
		if ppErr != nil {
			t.Logf("preprocess %s: %v", f.src, ppErr)
		}
		lines := strings.Split(src, "\n")
		t.Logf("=== %s (%d preprocessed lines, error at %d)", f.src, len(lines), f.line)
		lo := f.line - 5
		if lo < 0 {
			lo = 0
		}
		hi := f.line + 5
		if hi > len(lines) {
			hi = len(lines)
		}
		for i := lo; i < hi; i++ {
			t.Logf("%5d: %s", i+1, lines[i])
		}
	}
}

// preprocessToFile preprocesses srcPath and writes to a temp file, returning the path.
// Used for debugging parse errors by examining the preprocessed output.
func preprocessToFile(srcPath string, includePaths []string) (string, error) {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	pp := newPreprocessor(includePaths, nil)
	src, err := pp.Preprocess(string(raw), srcPath)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "gaston-pp-*.cm")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := tmp.WriteString(src); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

// TestLibmCommonCompile compiles every picolibc libm/common/*.c source file.
// It reports failures but does not archive — some files are data-only or
// depend on features still being added.
func TestLibmCommonCompile(t *testing.T) {
	entries, err := os.ReadDir("libm/common")
	if err != nil {
		t.Fatalf("read libm/common: %v", err)
	}
	includePaths := libmIncludePaths()

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		src := "libm/common/" + e.Name()
		obj := fmt.Sprintf("/tmp/libm-common-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// tinystdioSkip is the set of tinystdio .c files excluded from TestTinystdioCompile.
var tinystdioSkip = map[string]bool{
	"conv_flt.c": true, // template file #include'd by vfscanf.c, not independently compilable
}

// tinysydioPosixIO is the set of tinystdio files that require -DPOSIX_IO.
var tinystdioPosixIO = map[string]bool{
	"fdopen.c":   true,
	"fmemopen.c": true,
	"fopen.c":    true,
	"freopen.c":  true,
	"posixiob.c": true,
}

// tinystdioIncludePaths returns the include search paths for picolibc tinystdio sources.
func tinystdioIncludePaths() []string {
	tsdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/tinystdio"
	return []string{
		tsdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// TestTinystdioCompile compiles every picolibc tinystdio/*.c source file (minus the
// known-skipped files) and verifies gaston can parse and codegen each one.
// Files requiring POSIX_IO are compiled with -DPOSIX_IO.
func TestTinystdioCompile(t *testing.T) {
	tsdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/tinystdio"
	entries, err := os.ReadDir(tsdir)
	if err != nil {
		t.Fatalf("read tinystdio dir: %v", err)
	}
	includePaths := tinystdioIncludePaths()

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if tinystdioSkip[e.Name()] {
			continue
		}
		src := tsdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/tinystdio-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		var defines []string
		if tinystdioPosixIO[e.Name()] {
			defines = append(defines, "POSIX_IO")
		}
		// vfscanf/vfscanff include conv_flt.c which leaks #define base 10
		// unless _WANT_IO_C99_FORMATS makes it a local variable instead.
		switch e.Name() {
		case "vfscanf.c", "vfscanff.c":
			defines = append(defines, "_WANT_IO_C99_FORMATS=1")
		}
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// TestLibc compiles programs against libgastonc.a (the gaston standard C library)
// and runs them in an Alpine ARM64 container.
func TestLibc(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}

	libPath := buildLibgastonc(t)
	includePaths := []string{"libc"}

	for _, tt := range libcTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Compile the main program with the libc include path.
			mainSrc := fmt.Sprintf("testdata/%s.cm", tt.name)
			mainObj := fmt.Sprintf("/tmp/gaston-test-%s.o", tt.name)
			t.Cleanup(func() { os.Remove(mainObj) })
			if err := compileObjPath(mainSrc, mainObj, includePaths); err != nil {
				t.Fatalf("compile %s: %v", tt.name, err)
			}

			// Link: main.o + libgastonc.a → binary (lazy linking).
			binPath := fmt.Sprintf("/tmp/gaston-test-%s", tt.name)
			t.Cleanup(func() { os.Remove(binPath) })
			if err := link(binPath, []string{mainObj, libPath}); err != nil {
				t.Fatalf("link: %v", err)
			}

			// Run in Alpine ARM64 container.
			cmd := exec.Command("docker", "run", "--rm",
				"--platform", "linux/arm64",
				"-i",
				"-v", binPath+":/prog",
				"alpine:latest",
				"/prog",
			)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("docker run failed (exit %d):\nstdout: %s\nstderr: %s",
						ee.ExitCode(), string(out), string(ee.Stderr))
				}
				t.Fatalf("docker run: %v", err)
			}

			got := string(out)
			if got != tt.want {
				t.Errorf("output mismatch:\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}

// semErrorTest describes a program that must fail semCheck with a specific error.
type semErrorTest struct {
	name string // testdata/<name>.cm
	want string // expected substring in the semCheck error
}

var semErrorTests = []semErrorTest{
	// ── Item 7: pointer assignment type checking ──────────────────────────
	// Assigning int* to char* is incompatible (neither is void*).
	{name: "err_ptr_incompat", want: "assignment of incompatible pointer types"},
	// Assigning a non-zero integer to a pointer is not allowed.
	{name: "err_ptr_int", want: "assignment of non-pointer to pointer type"},
	// Assigning double* to int* is incompatible (FP pointer types are not interchangeable).
	{name: "err_ptr_fp", want: "assignment of incompatible pointer types"},
	// ── Item 9: const pointer target ─────────────────────────────────────
	// Assigning through a const-qualified pointer must be rejected.
	{name: "err_const_ptr", want: "assignment to const-qualified pointer target"},

	// ── Parameterised TypePtr / CType byzantine type-checking ─────────────
	// err_typedef_mismatch: IntPtr(=int*) and CharPtr(=char*) look like opaque
	// pointer aliases but must remain incompatible after typedef resolution.
	{name: "err_typedef_mismatch", want: "assignment of incompatible pointer types"},
	// err_double_ptr_inner: int** and char** are both depth-2 pointers, but the
	// inner element type differs; ctypeEq must recurse to the leaf to catch this.
	{name: "err_double_ptr_inner", want: "assignment of incompatible pointer types"},
	// err_struct_ptr_alias: CatPtr(=Cat*) and DogPtr(=Dog*) have identical layouts
	// but different struct tags; pointer compatibility is by name, not structure.
	{name: "err_struct_ptr_alias", want: "assignment of incompatible pointer types"},
	// err_typedef_chain_depth: T2 and U2 both expand to "double pointer" but with
	// int vs char at the leaf; the checker must follow the full typedef chain.
	{name: "err_typedef_chain_depth", want: "assignment of incompatible pointer types"},
	// err_func_typedef_arg: process() expects AlphaPtr(=Alpha*) but is called with
	// BetaPtr(=Beta*); call-site type check must resolve both typedef chains.
	{name: "err_func_typedef_arg", want: "incompatible pointer types"},
}

// TestSemErrors verifies that ill-typed programs are rejected by semCheck with
// the expected error message.
func TestSemErrors(t *testing.T) {
	for _, tt := range semErrorTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			srcPath := fmt.Sprintf("testdata/%s.cm", tt.name)
			raw, err := os.ReadFile(srcPath)
			if err != nil {
				t.Fatalf("read %s: %v", srcPath, err)
			}
			pp := newPreprocessor(nil, nil)
			src, err2 := pp.Preprocess(string(raw), srcPath)
			if err2 != nil {
				t.Fatalf("preprocess: %v", err2)
			}
			lex := newLexer(src, srcPath)
			yyParse(lex)
			if lex.errors > 0 {
				t.Fatalf("parse errors in %s", tt.name)
			}
			semErr := semCheck(lex.result, false)
			if semErr == nil {
				t.Fatalf("%s: expected semCheck error containing %q, got none", tt.name, tt.want)
			}
			if !strings.Contains(semErr.Error(), tt.want) {
				t.Errorf("%s: error %q does not contain %q", tt.name, semErr.Error(), tt.want)
			}
		})
	}
}

// timeIncludePaths returns the include search paths for picolibc time/ sources.
func timeIncludePaths() []string {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/time"
	return []string{
		tdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// timeSkip lists time/ source files that are not yet supported.
var timeSkip = map[string]bool{}

// TestTimeCompile compiles every picolibc time/*.c source file and verifies
// gaston can parse and codegen each one.
func TestTimeCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/time"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read time dir: %v", err)
	}
	includePaths := timeIncludePaths()
	defines := []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if timeSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/time-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// stringSkip lists string/ source files that are not yet supported.
var stringSkip = map[string]bool{
	"strdup_r.c":       true, // calls _malloc_r (picolibc reentrant malloc)
	"strndup_r.c":      true, // calls _malloc_r (picolibc reentrant malloc)
	"strerror_r.c":     true, // calls _strerror_r (defined in strerror.c, cross-file link dependency)
	"xpg_strerror_r.c": true, // calls _strerror_r (same)
}

// stringIncludePaths returns the include search paths for picolibc string/ sources.
func stringIncludePaths() []string {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/string"
	return []string{
		tdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// TestStringCompile compiles every picolibc string/*.c source file and verifies
// gaston can parse and codegen each one.
func TestStringCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/string"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read string dir: %v", err)
	}
	includePaths := stringIncludePaths()
	defines := []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if stringSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/string-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// ctypeSkip lists ctype/ source files that are not yet supported.
var ctypeSkip = map[string]bool{}

// ctypeIncludePaths returns the include search paths for picolibc ctype/ sources.
func ctypeIncludePaths() []string {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/ctype"
	return []string{
		tdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/locale",
	}
}

// TestCtypeCompile compiles every picolibc ctype/*.c source file and verifies
// gaston can parse and codegen each one.
func TestCtypeCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/ctype"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read ctype dir: %v", err)
	}
	includePaths := ctypeIncludePaths()
	defines := []string{"__PICOLIBC__=1", "_LIBC=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if ctypeSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/ctype-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// searchSkip lists search/ source files that are not yet supported.
var searchSkip = map[string]bool{
	"hcreate_r.c": true, // sizeof *ptr (no parens)
	"ndbm.c":      true, // function pointer inside struct typedef
	"tdelete.c":   true, // double-pointer locals + dereference
	"tdestroy.c":  true, // double-pointer locals + dereference
	"tfind.c":     true, // double-pointer locals + dereference
	"tsearch.c":   true, // double-pointer locals + dereference
	"twalk.c":     true, // double-pointer locals + dereference
}

// searchIncludePaths returns the include search paths for picolibc search/ sources.
func searchIncludePaths() []string {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/search"
	return []string{
		tdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// TestSearchCompile compiles every picolibc search/*.c source file and verifies
// gaston can parse and codegen each one.
func TestSearchCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/search"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read search dir: %v", err)
	}
	includePaths := searchIncludePaths()
	defines := []string{"__PICOLIBC__=1", "_LIBC=1", "_SEARCH_PRIVATE=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if searchSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/search-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// miscIncludePaths returns the include search paths for picolibc misc/ sources.
func miscIncludePaths() []string {
	return []string{
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// TestMiscCompile compiles every picolibc misc/*.c source file.
func TestMiscCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/misc"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read misc dir: %v", err)
	}
	includePaths := miscIncludePaths()
	defines := []string{"__PICOLIBC__=1", "_LIBC=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/misc-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// argzSkip lists argz/ source files that are not yet supported.
var argzSkip = map[string]bool{
	"argz_create.c": true, // cannot dereference non-pointer (double-pointer pattern)
}

// TestArgzCompile compiles every picolibc argz/*.c source file.
func TestArgzCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/argz"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read argz dir: %v", err)
	}
	includePaths := miscIncludePaths()
	defines := []string{"__PICOLIBC__=1", "_LIBC=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if argzSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/argz-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// stdlibSkip lists stdlib/ source files that are not yet supported.
var stdlibSkip = map[string]bool{}

// stdlibIncludePaths returns the include search paths for picolibc stdlib/ sources.
func stdlibIncludePaths() []string {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/stdlib"
	return []string{
		tdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// TestStdlibCompile compiles every picolibc stdlib/*.c source file and verifies
// gaston can parse and codegen each one.
func TestStdlibCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/stdlib"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read stdlib dir: %v", err)
	}
	includePaths := stdlibIncludePaths()
	defines := []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1", "_LIBC=1", "__SINGLE_THREAD=1", "TINY_STDIO=1", "MALLOC_PROVIDED=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if stdlibSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/stdlib-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// localeSkip lists locale/ source files that are not yet supported.
var localeSkip = map[string]bool{}

// localeIncludePaths returns the include search paths for picolibc locale/ sources.
func localeIncludePaths() []string {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/locale"
	return []string{
		tdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// TestLocaleCompile compiles every picolibc locale/*.c source file and verifies
// gaston can parse and codegen each one.
func TestLocaleCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/locale"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read locale dir: %v", err)
	}
	includePaths := localeIncludePaths()
	defines := []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if localeSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/locale-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// posixSkip lists posix/ source files that are not yet supported.
var posixSkip = map[string]bool{
	"engine.c":  true, // template file #include'd by regexec.c, not independently compilable
	"regexec.c": true, // regmatch_t array param type mismatch in smatcher call
}

// posixIncludePaths returns the include search paths for picolibc posix/ sources.
func posixIncludePaths() []string {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/posix"
	return []string{
		tdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// TestPosixCompile compiles every picolibc posix/*.c source file and verifies
// gaston can parse and codegen each one.
func TestPosixCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/posix"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read posix dir: %v", err)
	}
	includePaths := posixIncludePaths()
	defines := []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if posixSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/posix-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// signalSkip lists signal/ source files that are not yet supported.
var signalSkip = map[string]bool{}

// signalIncludePaths returns the include search paths for picolibc signal/ sources.
func signalIncludePaths() []string {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/signal"
	return []string{
		tdir,
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
	}
}

// TestSignalCompile compiles every picolibc signal/*.c source file and verifies
// gaston can parse and codegen each one.
func TestSignalCompile(t *testing.T) {
	tdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/signal"
	entries, err := os.ReadDir(tdir)
	if err != nil {
		t.Fatalf("read signal dir: %v", err)
	}
	includePaths := signalIncludePaths()
	defines := []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"}

	passed := 0
	var failed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		if signalSkip[e.Name()] {
			continue
		}
		src := tdir + "/" + e.Name()
		obj := fmt.Sprintf("/tmp/signal-%s.o", strings.TrimSuffix(e.Name(), ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths, defines...); err != nil {
			t.Logf("FAIL %s: %v", e.Name(), err)
			failed = append(failed, e.Name())
			continue
		}
		passed++
	}
	t.Logf("%d passed, %d failed", passed, len(failed))
	if len(failed) > 0 {
		t.Errorf("failed files: %v", failed)
	}
}

// TestDockerRun compiles each test program and runs it in an Alpine ARM64
// container, comparing stdout to the expected string.
func TestDockerRun(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}

	for _, tt := range featureTests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			binPath := fmt.Sprintf("/tmp/gaston-test-%s", tt.name)
			t.Cleanup(func() { os.Remove(binPath) })

			if err := compileTest(tt.name, binPath); err != nil {
				t.Fatalf("compile: %v", err)
			}

			cmd := exec.Command("docker", "run", "--rm",
				"--platform", "linux/arm64",
				"-i",
				"-v", binPath+":/prog",
				"alpine:latest",
				"/prog",
			)
			cmd.Stdin = strings.NewReader(tt.stdin)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("docker run failed (exit %d):\nstdout: %s\nstderr: %s",
						ee.ExitCode(), string(out), string(ee.Stderr))
				}
				t.Fatalf("docker run: %v", err)
			}

			got := string(out)
			if got != tt.want {
				t.Errorf("output mismatch:\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}

// TestTinystdioRun compiles picolibc's tinystdio snprintf chain with gaston,
// links with a test program, and runs in Docker to verify function-pointer-
// based stdio works end to end.
func TestTinystdioRun(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}

	tsdir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/tinystdio"
	includePaths := tinystdioIncludePaths()

	// Minimal set of tinystdio .c files needed for snprintf.
	tsFiles := []string{
		"snprintf.c",
		"vfprintf.c",
		"filestrput.c",
		"dtoa_engine.c",
		"dtoa_data.c",
	}
	var tsObjs []string
	for _, f := range tsFiles {
		src := tsdir + "/" + f
		obj := fmt.Sprintf("/tmp/gaston-ts-%s.o", strings.TrimSuffix(f, ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, includePaths,
			"__PICOLIBC__=1", "TINY_STDIO=1", "FORMAT_DEFAULT_DOUBLE=1"); err != nil {
			t.Fatalf("compile tinystdio %s: %v", f, err)
		}
		tsObjs = append(tsObjs, obj)
	}

	// Picolibc string functions (strcmp, strlen).
	stringDir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/string"
	for _, f := range []string{"strcmp.c", "strlen.c"} {
		src := stringDir + "/" + f
		obj := fmt.Sprintf("/tmp/gaston-ts-str-%s.o", strings.TrimSuffix(f, ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, stringIncludePaths(),
			"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"); err != nil {
			t.Fatalf("compile string %s: %v", f, err)
		}
		tsObjs = append(tsObjs, obj)
	}

	// Gaston's libc for print_char/errno.
	libPath := buildLibgastonc(t)

	// Compile the test program.
	testSrc := "testdata/tinystdio_snprintf.cm"
	testObj := "/tmp/gaston-ts-test.o"
	t.Cleanup(func() { os.Remove(testObj) })
	if err := compileObjPath(testSrc, testObj, nil); err != nil {
		t.Fatalf("compile test: %v", err)
	}

	// Link everything.
	binPath := "/tmp/gaston-ts-test"
	t.Cleanup(func() { os.Remove(binPath) })
	linkInputs := append([]string{testObj}, tsObjs...)
	linkInputs = append(linkInputs, libPath)
	if err := link(binPath, linkInputs); err != nil {
		t.Fatalf("link: %v", err)
	}

	// Run in Docker.
	cmd := exec.Command("docker", "run", "--rm",
		"--platform", "linux/arm64",
		"-i",
		"-v", binPath+":/prog",
		"alpine:latest",
		"/prog",
	)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("docker run failed (exit %d):\nstdout: %s\nstderr: %s",
				ee.ExitCode(), string(out), string(ee.Stderr))
		}
		t.Fatalf("docker run: %v", err)
	}

	want := "A\nB\nhello\n"
	// snprintf("hello") → buf contains "hello", printed char-by-char
	if got := string(out); got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestPicolibcRun compiles picolibc test programs with gaston, links them
// against gaston's libc and picolibc string functions, and runs in Docker.
func TestPicolibcRun(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}

	// Build gaston's libc archive (printf, snprintf, errno, etc.)
	libPath := buildLibgastonc(t)

	// Picolibc string functions needed by the test programs.
	stringDir := "/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/string"
	stringFuncs := []string{"strcmp.c", "strlen.c", "strcpy.c"}
	var stringObjs []string
	for _, f := range stringFuncs {
		src := stringDir + "/" + f
		obj := fmt.Sprintf("/tmp/gaston-ptest-%s.o", strings.TrimSuffix(f, ".c"))
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath(src, obj, stringIncludePaths(),
			"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"); err != nil {
			t.Fatalf("compile %s: %v", f, err)
		}
		stringObjs = append(stringObjs, obj)
	}

	testDir := "/Users/iansmith/wazero/tinygo/lib/picolibc/test/libc-testsuite"
	testIncludePaths := []string{
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/tinystdio",
		"libm/include",
		"/Users/iansmith/wazero/tinygo/lib/picolibc/newlib/libc/include",
		testDir,
	}
	testDefines := []string{"__PICOLIBC__=1", "TINY_STDIO=1"}

	tests := []struct {
		name string // source file base name without .c
		want string // expected stdout
	}{
		{name: "snprintf", want: "snprintf test passed\n"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			src := testDir + "/" + tt.name + ".c"
			obj := fmt.Sprintf("/tmp/gaston-ptest-%s.o", tt.name)
			t.Cleanup(func() { os.Remove(obj) })
			if err := compileObjPath(src, obj, testIncludePaths, testDefines...); err != nil {
				t.Fatalf("compile %s: %v", tt.name, err)
			}

			binPath := fmt.Sprintf("/tmp/gaston-ptest-%s", tt.name)
			t.Cleanup(func() { os.Remove(binPath) })
			linkInputs := append([]string{obj}, stringObjs...)
			linkInputs = append(linkInputs, libPath)
			if err := link(binPath, linkInputs); err != nil {
				t.Fatalf("link: %v", err)
			}

			cmd := exec.Command("docker", "run", "--rm",
				"--platform", "linux/arm64",
				"-i",
				"-v", binPath+":/prog",
				"alpine:latest",
				"/prog",
			)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("docker run failed (exit %d):\nstdout: %s\nstderr: %s",
						ee.ExitCode(), string(out), string(ee.Stderr))
				}
				t.Fatalf("docker run: %v", err)
			}

			got := string(out)
			if got != tt.want {
				t.Errorf("output mismatch:\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}
