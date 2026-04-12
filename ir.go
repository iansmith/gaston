package main

import "fmt"

// IROpCode is a three-address IR instruction opcode.
type IROpCode int

const (
	IRAdd  IROpCode = iota // Dst = Src1 + Src2
	IRSub                  // Dst = Src1 - Src2
	IRMul                  // Dst = Src1 * Src2
	IRDiv                  // Dst = Src1 / Src2
	IRMod                  // Dst = Src1 % Src2
	IRBitAnd               // Dst = Src1 & Src2
	IRBitOr                // Dst = Src1 | Src2
	IRBitXor               // Dst = Src1 ^ Src2
	IRBitNot               // Dst = ~Src1
	IRShl                  // Dst = Src1 << Src2
	IRShr                  // Dst = Src1 >> Src2 (arithmetic)
	IRCopy                 // Dst = Src1
	IRLoad                 // Dst = Src1[Src2]   (array element load)
	IRStore                // Dst[Src1] = Src2   (array element store)
	IRGetAddr              // Dst = &Src1        (base address of array)
	IRLabel                // Extra: label name
	IRJump                 // goto Extra
	IRJumpT                // if Src1 != 0 goto Extra
	IRJumpF                // if Src1 == 0 goto Extra
	IRLabelAddr            // Dst = address of user label; Extra = label name (without prefix)
	IRIndirectJump         // goto *Src1 (branch to address in Src1; computed goto)
	IRFrameAddr            // Dst = current frame pointer (__builtin_frame_address(0))
	IRLt                   // Dst = (Src1 < Src2)
	IRLe                   // Dst = (Src1 <= Src2)
	IRGt                   // Dst = (Src1 > Src2)
	IRGe                   // Dst = (Src1 >= Src2)
	IREq                   // Dst = (Src1 == Src2)
	IRNe                   // Dst = (Src1 != Src2)
	IRParam                // push Src1 as next call argument
	IRCall                 // Dst = Extra(Src1.IVal args)
	IRReturn               // return Src1 (Src1.Kind==AddrNone → void)
	IREnter                // function entry marker; Extra = name
	IRStrAddr              // Dst = address of string literal; Extra = label
	IRDerefLoad            // Dst = *Src1  (load 8 bytes via pointer)
	IRDerefStore           // *Dst = Src1  (store 8 bytes via pointer)
	IRFDerefLoad           // Dst = *Src1  (load 8-byte double via pointer; result is FP)
	IRFDerefStore          // *Dst = Src1  (store 8-byte double via pointer; Src1 is FP)
	IRDerefCharLoad        // Dst = *(byte*)Src1  (1-byte load via computed pointer; zero-extended)
	IRDerefCharStore       // *(byte*)Dst = Src1  (1-byte store via computed pointer)
	IRCharLoad             // Dst = Src1[Src2]  (char* subscript — byte load, no scaling)
	IRCharStore            // Dst[Src1] = Src2  (char* subscript — byte store, no scaling)

	// Unsigned integer operations (operands treated as unsigned).
	IRUDiv  // Dst = Src1 / Src2   (unsigned divide)
	IRUMod  // Dst = Src1 % Src2   (unsigned modulo)
	IRUShr  // Dst = Src1 >> Src2  (logical/unsigned shift right)
	IRULt   // Dst = (Src1 <  Src2) unsigned
	IRULe   // Dst = (Src1 <= Src2) unsigned
	IRUGt   // Dst = (Src1 >  Src2) unsigned
	IRUGe   // Dst = (Src1 >= Src2) unsigned

	// Floating-point operations (64-bit double precision).
	IRFAdd        // Dst = Src1 + Src2  (double)
	IRFSub        // Dst = Src1 - Src2  (double)
	IRFMul        // Dst = Src1 * Src2  (double)
	IRFDiv        // Dst = Src1 / Src2  (double)
	IRFNeg        // Dst = -Src1        (double)
	IRFCopy       // Dst = Src1         (FP copy via D registers)
	IRFLt         // Dst = (Src1 <  Src2) → int (ordered, false for NaN)
	IRFLe         // Dst = (Src1 <= Src2) → int (ordered, false for NaN)
	IRFGt         // Dst = (Src1 >  Src2) → int (ordered, false for NaN)
	IRFGe         // Dst = (Src1 >= Src2) → int (ordered, false for NaN)
	IRFEq         // Dst = (Src1 == Src2) → int (false for NaN)
	IRFNe         // Dst = (Src1 != Src2) → int (true  for NaN)
	IRIntToDouble // Dst = (double)Src1   (int64 → double)
	IRDoubleToInt // Dst = (int64)Src1    (double → int64, truncate toward zero)
	IRFParam      // push Src1 as next FP call argument (into D0-D7)

	// Struct field operations.
	// IRFieldLoad:  Dst = *(Src1 + Src2.IVal)  — load field at byte offset Src2.IVal
	// IRFieldStore: *(Dst + Src2.IVal) = Src1  — store value Src1 at field byte offset Src2.IVal
	IRFieldLoad   // Dst = field load (int): base ptr in Src1, byte offset in Src2.IVal
	IRFieldStore  // field store (int): base ptr in Dst, value in Src1, byte offset in Src2.IVal
	IRFFieldLoad  // Dst = field load (double): base ptr in Src1, byte offset in Src2.IVal
	IRFFieldStore // field store (double): base ptr in Dst, value in Src1, byte offset in Src2.IVal

	// IRAddrOf: Dst = storage-address of Src1 — always SP/FP+offset for locals,
	// global VA for globals. Unlike IRGetAddr, never loads through a pointer slot.
	// Used exclusively by KindAddrOf (&var) in irgen.go.
	IRAddrOf

	// Variable-length array allocation.
	// IRVLAAlloc: Dst = alloca(Src1 * 8) — allocate Src1 elements on the stack,
	// store the base pointer in the frame slot Dst.  The function must use
	// FP-relative addressing for all static frame slots once any VLA has been
	// allocated (SP may differ from FP after this point).
	IRVLAAlloc

	// Integer promotion (C usual arithmetic conversions).
	// Src2.IVal is the bit width (8 for char, 16 for short).
	// IRSignExtend: Dst = sign_extend(Src1, Src2.IVal) — signed promotion to 64-bit int
	// IRZeroExtend: Dst = zero_extend(Src1, Src2.IVal) — unsigned promotion to 64-bit int
	IRSignExtend
	IRZeroExtend

	// Function pointer operations.
	// IRFuncAddr:    Dst = VA of function named Extra (for fp = funcname; assignments)
	// IRFuncPtrCall: Dst = (*Src1)(args); Src1 holds the function pointer value
	IRFuncAddr
	IRFuncPtrCall

	// Bit-pattern moves between FP and integer registers (no value conversion).
	// Used at variadic call sites to pass double arguments through X registers so
	// the callee's integer-only register-save area captures them correctly.
	// IRFBitcastFI: Dst(int64)  = bit_pattern(Src1 double) — FMOV Xd, Dn
	// IRFBitcastIF: Dst(double) = bit_pattern(Src1 int64)  — FMOV Dd, Xn
	IRFBitcastFI
	IRFBitcastIF

	// Struct-to-struct copy (struct assignment: x = y).
	// Dst and Src1 are both AddrLocal/AddrGlobal addresses of struct storage.
	// StructTag names the struct type (for size lookup).
	// Copies SizeBytes(StructTag)/8 consecutive 8-byte words.
	IRStructCopy

	// Bit-manipulation intrinsics — emit inline ARM64 instructions.
	// Src1 is the input value. Dst receives the result (TypeInt).
	// For IRCLZ/IRCTZ of 32-bit __builtin_clz (non-l/ll): TypeHint==TypeUnsignedInt
	//   signals that the upper 32 bits must be zeroed before CLZ (GCC compat).
	IRCLZ      // count leading zeros  — ARM64: CLZ Xd, Xn (or Wd, Wn for 32-bit)
	IRCTZ      // count trailing zeros — ARM64: RBIT Xd, Xn; CLZ Xd, Xd
	IRPopcount // population count     — ARM64: FMOV D0,X0; CNT V0.8B,V0.8B; ADDV B0,V0.8B; UMOV X0,V0.B[0]
	IRFfs      // find first set bit   — ARM64: CMP X0,#0; RBIT X1,X0; CLZ X1,X1; ADD X1,X1,#1; CSEL X0,XZR,X1,EQ

	// ── 128-bit integer arithmetic ─────────────────────────────────────────────
	// All Dst, Src1, Src2 refer to the lo-half IRAddr of a 128-bit local.
	// elfgen derives the hi-half address as offsets[addr.Name] + 8.

	// Arithmetic (result is 128-bit)
	IR128Add  // Dst = Src1 + Src2   (ADDS lo; ADC hi)
	IR128Sub  // Dst = Src1 - Src2   (SUBS lo; SBC hi)
	IR128Mul  // Dst = Src1 * Src2   (MUL lo; UMULH/SMULH hi for unsigned/signed)
	IR128And  // Dst = Src1 & Src2   (AND both halves)
	IR128Or   // Dst = Src1 | Src2   (ORR both halves)
	IR128Xor  // Dst = Src1 ^ Src2   (EOR both halves)
	IR128Neg  // Dst = -Src1         (NEGS lo; NGC hi)

	// Shifts — Src2.IVal is the shift count (constant only for now)
	IR128Shl  // Dst = Src1 << Src2.IVal  (logical left)
	IR128LShr // Dst = Src1 >> Src2.IVal  (logical right, for unsigned)
	IR128AShr // Dst = Src1 >> Src2.IVal  (arithmetic right, for signed)

	// Comparisons → 32-bit int result (0 or 1)
	IR128Eq  // Dst = (Src1 == Src2)   signed/unsigned same
	IR128Ne  // Dst = (Src1 != Src2)
	IR128ULt // Dst = (Src1 < Src2)    unsigned
	IR128ULe // Dst = (Src1 <= Src2)   unsigned
	IR128UGt // Dst = (Src1 > Src2)    unsigned
	IR128UGe // Dst = (Src1 >= Src2)   unsigned
	IR128SLt // Dst = (Src1 < Src2)    signed (hi half is sign-extended)
	IR128SLe // Dst = (Src1 <= Src2)   signed
	IR128SGt // Dst = (Src1 > Src2)    signed
	IR128SGe // Dst = (Src1 >= Src2)   signed

	// Copy
	IR128Copy // Dst = Src1  (copies both halves)

	// Widening / narrowing casts
	IR128FromI64 // Dst (128-bit) = sign_extend(Src1 (int64)): lo = Src1; hi = ASR Src1, 63
	IR128FromU64 // Dst (128-bit) = zero_extend(Src1 (uint64)): lo = Src1; hi = 0
	IR64From128  // Dst (int64/uint64) = Src1_lo  (narrow; hi discarded)

	// alloca() stack allocation — byte-granular (unlike IRVLAAlloc which is word-granular).
	// Src1 = byte count. Dst = frame slot that receives the allocated pointer.
	// The function's HasVLA flag is set so FP-relative addressing is used.
	IRAllocaAlloc

	// Byte-swap (bswap) intrinsics — emit REV16/REV32/REV64 instructions.
	// Src1 = input value. Dst = result.
	// TypeHint distinguishes: TypeUnsignedShort=16-bit, TypeUnsignedInt=32-bit, TypeUnsignedLong=64-bit.
	IRBswap

	// Overflow-checked arithmetic — __builtin_add_overflow(a, b, *result).
	// Dst = sum result temp. Src1 = a, Src2 = b.
	// Extra = name of overflow-flag temp (allocated by irgen, stored here by elfgen).
	// A subsequent IRDerefStore stores the sum to *ptr.
	IRAddOverflow
	IRSubOverflow
	IRMulOverflow

	// Floating-point classification and manipulation intrinsics.
	// IRFIsNaN:    Dst(int) = 1 if Src1(double) is NaN, else 0.
	//              ARM64: FCMP Dn,Dn; CSET Xd,VS
	// IRFIsInf:    Dst(int) = 1 if |Src1(double)| == +inf, else 0.
	//              ARM64: FABS Dm,Dn; MOVZ Xt,0x7FF0,LSL#48; FMOV Dk,Xt; FCMP Dm,Dk; CSET Xd,EQ
	// IRFCopySign: Dst(double) = magnitude of Src1 with sign of Src2.
	//              ARM64: bit-level sign-bit splice via FMOV(to GP), AND/ORR masks, FMOV(from GP)
	IRFIsNaN
	IRFIsInf
	IRFCopySign
)

// AddrKind identifies what an IR address refers to.
type AddrKind int

const (
	AddrNone   AddrKind = iota // unused / void
	AddrConst                  // integer constant (IVal)
	AddrTemp                   // compiler-generated temporary (Name = "tN")
	AddrLocal                  // local variable or parameter (Name = identifier)
	AddrGlobal                 // global variable (Name = identifier)
	AddrFConst                 // floating-point constant (FVal = value, Name = pool label)
)

// IRAddr is one operand in a three-address IR instruction.
type IRAddr struct {
	Kind AddrKind
	IVal int     // for AddrConst
	FVal float64 // for AddrFConst
	Name string  // for AddrTemp, AddrLocal, AddrGlobal, AddrFConst (pool label)
}

func (a IRAddr) String() string {
	switch a.Kind {
	case AddrNone:
		return "_"
	case AddrConst:
		return fmt.Sprintf("%d", a.IVal)
	case AddrFConst:
		return fmt.Sprintf("%g(%s)", a.FVal, a.Name)
	default:
		return a.Name
	}
}

// Quad is one three-address IR instruction.
type Quad struct {
	Op        IROpCode
	Dst       IRAddr
	Src1      IRAddr
	Src2      IRAddr
	Extra     string   // label name (IRLabel/IRJump/IRJumpT/IRJumpF) or function name (IRCall/IREnter)
	TypeHint  TypeKind // for IRFieldLoad/IRFieldStore: field TypeKind; for IRCall/IRReturn: TypeStruct signals struct-by-value
	StructTag string   // non-empty when TypeHint == TypeStruct: the struct type name for size lookup
}

// IRGlobal describes one global variable declaration.
type IRGlobal struct {
	Name       string
	IsArr      bool
	IsPtr      bool    // true for TypePtr globals
	IsStruct   bool    // true for TypeStruct globals
	Pointee    *CType  // non-nil when IsPtr: full pointee type
	StructTag  string  // struct type name (when IsStruct)
	IsExtern    bool   // true for extern-declared globals (no storage allocated)
	IsStatic    bool   // true for static globals (internal linkage; STB_LOCAL in ELF)
	IsWeak      bool   // true for __attribute__((weak)) globals
	SectionName string // non-empty for __attribute__((section("name")))
	Align       int    // non-zero for _Alignas(N): required alignment in bytes
	Size       int     // 1 for scalar, N for array[N] or struct (N = numFields)
	InnerDim   int     // inner dimension for 2D arrays (0 for 1D or non-array)
	HasInitVal bool
	InitVal    int    // constant initializer value (only when HasInitVal && !IsArr && InitData==nil)
	InitData   []byte // byte-buffer constant initializer (struct/array init list; nil = zero-filled)
	InitRelocs []InitReloc // pointer relocations within InitData (string literals, etc.)
}

// InitReloc records that the address of Label should be stored at ByteOff
// within a global's InitData during _start initialization.
type InitReloc struct {
	ByteOff int    // byte offset within the global's storage
	Label   string // label whose address should be stored (e.g. "str0")
	Addend  int64  // constant addend (e.g. for &arr[i] - N)
}

// IRLocal describes one local variable in a function (not a parameter).
type IRLocal struct {
	Name      string
	IsArray   bool
	IsPtr     bool    // true for TypePtr locals
	IsStruct  bool    // true for TypeStruct locals
	IsVLA     bool    // true for variable-length array (runtime size; pointer slot in frame)
	Is128     bool    // true for TypeInt128/TypeUint128 locals (2-slot, 16-byte)
	Pointee   *CType  // non-nil when IsPtr: full pointee type
	StructTag string  // struct type name (when IsStruct)
	ArrSize   int      // 1 for scalar, N for int x[N]; for struct: number of fields; 0 for VLA
	ElemType  TypeKind // element type for arrays (TypeChar → 1 byte, TypeInt → 4 bytes, etc.; 0 → 8 bytes)
	Align     int      // non-zero for _Alignas(N): required alignment in bytes
}

// IRFunc is the IR for one function.
type IRFunc struct {
	Name            string
	ReturnType      TypeKind   // return type (TypeVoid if void)
	ReturnPointee   *CType     // non-nil when ReturnType == TypePtr: full pointee type
	ReturnStructTag string     // non-empty when ReturnType == TypeStruct: struct type name
	Params          []string   // parameter names in declaration order (no "..." marker)
	ParamType       []TypeKind // corresponding types
	ParamPointee    []*CType   // per-param pointee CType (nil for non-pointer params)
	ParamStructTag  []string   // parallel to ParamType; non-empty when ParamType[i]==TypeStruct
	IsVariadic   bool       // true if this function accepts variadic arguments
	HasVLA       bool       // true if any local is a VLA (requires FP-relative frame addressing)
	IsStatic     bool       // true for static functions (internal linkage; STB_LOCAL in ELF)
	IsWeak       bool       // true for __attribute__((weak)) functions
	SectionName  string     // non-empty for __attribute__((section("name")))
	Locals       []IRLocal  // local variables declared inside the function body
	Quads        []Quad
}

// IRStrLit is one string literal in the rodata section.
type IRStrLit struct {
	Label   string // synthetic label (e.g. "str0")
	Content string // string content (without NUL terminator; NUL is appended at output)
}

// IRFConst is one floating-point constant in the literal pool.
type IRFConst struct {
	Label string  // synthetic pool label (e.g. "fc0")
	Value float64 // IEEE 754 double value
}

// IRProgram is the complete IR for a program.
// IRAlias records one __attribute__((alias("target"))) declaration.
// The alias symbol will be emitted in the ELF object with the same value
// and section as the target, rather than as an undefined (SHN_UNDEF) reference.
type IRAlias struct {
	Name   string // the alias symbol name
	Target string // the target symbol name (must be defined in the same translation unit)
	IsFunc bool   // true for function aliases, false for variable aliases
}

type IRProgram struct {
	Globals    []IRGlobal
	Funcs      []*IRFunc
	Aliases    []IRAlias  // __attribute__((alias(...))) declarations
	StrLits    []IRStrLit // string literals (rodata)
	FConsts    []IRFConst // floating-point constants (literal pool entries)
	FuncRefs   []string   // names of user functions whose addresses are taken (IRFuncAddr)
	StructDefs map[string]*StructDef // struct type definitions (from ast.go)
}
