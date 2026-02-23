// grammar.js

// Precedence levels (Higher number = tighter binding)
const PREC = {
  ASSIGN: 1,
  RANGE: 2,
  LOGICAL_OR: 3,
  LOGICAL_AND: 4,
  BITWISE_OR: 5,
  BITWISE_XOR: 6,
  BITWISE_AND: 7,
  EQUALITY: 8,
  RELATIONAL: 9,
  SHIFT: 10,
  ADDITIVE: 11,
  MULTIPLICATIVE: 12,
  CAST: 13,
  UNARY: 14,
  POSTFIX: 15,
  TYPE_DOT: 16, // Forces namespace dots in types to bind tighter than field access
};

module.exports = grammar({
  name: 'synovium',

  extras: $ => [
    /\s/,
    $.comment,
  ],

  // Tree-sitter's GLR branching mechanism for unresolvable context overlaps
  conflicts: $ => [
    [$.primary_expr, $.struct_init_expr], // Resolves: if my_struct { ... }
  ],

  rules: {
    source_file: $ => repeat($.declaration),

    // --- 1. TOP LEVEL DECLARATIONS ---
    declaration: $ => choice(
      seq($.variable_decl, ';'),
      $.struct_decl,
      $.enum_decl,
      $.impl_decl,
      $.function_decl
    ),

    // --- 2. DATA STRUCTURES & IMPL ---
    struct_decl: $ => seq('struct', $.identifier, '{', optional($.field_decl_list), '}'),
    field_decl_list: $ => seq($.field_decl, repeat(seq(',', $.field_decl)), optional(',')),
    field_decl: $ => seq($.identifier, ':', $.type),

    enum_decl: $ => seq('enum', $.identifier, '{', optional($.variant_list), '}'),
    variant_list: $ => seq($.variant, repeat(seq(',', $.variant)), optional(',')),
    variant: $ => seq($.identifier, optional(seq('(', $.type_list, ')'))),

    impl_decl: $ => seq('impl', $.identifier, '{', repeat($.function_decl), '}'),

    // --- 3. FUNCTIONS & VARIABLES ---
    function_decl: $ => seq(
      'fnc', $.identifier, '(', optional($.parameter_list), ')',
      optional(seq($.return_op, $.type)),
      $.block
    ),
    parameter_list: $ => seq($.parameter, repeat(seq(',', $.parameter)), optional(',')),
    parameter: $ => seq($.identifier, ':', $.type),
    return_op: $ => choice(':=', '='),

    variable_decl: $ => seq($.identifier, ':', $.type, $.assign_op, $.expression),
    variable_decl_no_semi: $ => seq($.identifier, ':', $.type, $.assign_op, $.expression),
    assign_op: $ => choice('=', '~=', ':='),

    // --- 4. BLOCKS & STATEMENTS ---
    block: $ => seq('{', repeat($.statement), optional($.expression), '}'),

    statement: $ => choice(
      seq($.variable_decl, ';'),
      seq('ret', optional($.expression), ';'),
      seq('yld', optional($.expression), ';'),
      seq('brk', ';'),
      seq($.expression, ';')
    ),

    // --- 5. TYPES ---
    type: $ => seq(repeat('*'), repeat('&'), $.base_type),
    
    // Using prec.left(PREC.TYPE_DOT) forces the parser to group std.String as a type 
    // rather than (std).String as a postfix property access.
    base_type: $ => choice(
      prec.left(PREC.TYPE_DOT, seq($.identifier, repeat(seq('.', $.identifier)))),
      seq('[', $.type, ';', choice($.expression, ':'), ']')
    ),
    type_list: $ => seq($.type, repeat(seq(',', $.type))),

    // --- 6. EXPRESSIONS ---
    expression: $ => choice(
      $.assignment_expr,
      $.range_expr,
      $.logical_or_expr,
      $.logical_and_expr,
      $.bitwise_or_expr,
      $.bitwise_xor_expr,
      $.bitwise_and_expr,
      $.equality_expr,
      $.relational_expr,
      $.shift_expr,
      $.additive_expr,
      $.multiplicative_expr,
      $.cast_expr,
      $.unary_expr,
      $.postfix_expr,
      $.primary_expr
    ),

    assignment_expr: $ => prec.right(PREC.ASSIGN, seq($.expression, choice('=', '~='), $.expression)),
    
    range_expr: $ => prec.left(PREC.RANGE, seq($.expression, '...', $.expression)),
    
    logical_or_expr: $ => prec.left(PREC.LOGICAL_OR, seq($.expression, '||', $.expression)),
    logical_and_expr: $ => prec.left(PREC.LOGICAL_AND, seq($.expression, '&&', $.expression)),
    
    bitwise_or_expr: $ => prec.left(PREC.BITWISE_OR, seq($.expression, '|', $.expression)),
    bitwise_xor_expr: $ => prec.left(PREC.BITWISE_XOR, seq($.expression, '^', $.expression)),
    bitwise_and_expr: $ => prec.left(PREC.BITWISE_AND, seq($.expression, '&', $.expression)),
    
    equality_expr: $ => prec.left(PREC.EQUALITY, seq($.expression, choice('==', '!='), $.expression)),
    relational_expr: $ => prec.left(PREC.RELATIONAL, seq($.expression, choice('<', '<=', '>', '>='), $.expression)),
    shift_expr: $ => prec.left(PREC.SHIFT, seq($.expression, choice('<<', '>>'), $.expression)),
    additive_expr: $ => prec.left(PREC.ADDITIVE, seq($.expression, choice('+', '-'), $.expression)),
    multiplicative_expr: $ => prec.left(PREC.MULTIPLICATIVE, seq($.expression, choice('*', '/', '%'), $.expression)),
    
    cast_expr: $ => prec.left(PREC.CAST, seq($.expression, 'as', $.type)),
    
    unary_expr: $ => prec.left(PREC.UNARY, seq(choice('!', '~', '-', '*', '&'), $.expression)),
    
    postfix_expr: $ => prec.left(PREC.POSTFIX, seq($.expression, choice(
      seq('(', optional($.argument_list), ')'),
      seq('.', $.identifier),
      seq('[', choice($.expression, seq($.expression, '...', $.expression), ':'), ']'),
      '?'
    ))),

    // --- 7. PRIMARY EXPRESSIONS & CONTROL FLOW ---
    primary_expr: $ => choice(
      $.identifier,
      $.literal,
      seq('(', $.expression, ')'),
      $.struct_init_expr,
      $.if_expr,
      $.match_expr,
      $.loop_expr,
      $.block
    ),

    struct_init_expr: $ => seq($.identifier, '{', optional($.struct_init_list), '}'),
    struct_init_list: $ => seq($.struct_init_field, repeat(seq(',', $.struct_init_field)), optional(',')),
    struct_init_field: $ => seq('.', $.identifier, '=', $.expression),

    // Wrapped in prec.right to solve the "Dangling Else" problem
    if_expr: $ => prec.right(seq('if', $.expression, $.block, repeat(seq('elif', $.expression, $.block)), optional(seq('else', $.block)))),

    match_expr: $ => seq('match', $.expression, '{', repeat($.match_arm), '}'),
    match_arm: $ => seq($.pattern, '->', $.block, optional(',')),
    pattern: $ => seq($.identifier, repeat(seq('.', $.identifier)), optional(seq('(', $.identifier_list, ')'))),

    loop_expr: $ => seq('loop', optional(seq('(', $.loop_cond, ')')), $.block),
    loop_cond: $ => choice($.variable_decl_no_semi, $.expression),

    argument_list: $ => seq($.expression, repeat(seq(',', $.expression)), optional(',')),
    identifier_list: $ => seq($.identifier, repeat(seq(',', $.identifier)), optional(',')),

    // --- 8. TERMINALS (Lexicon) ---
    identifier: $ => /[a-zA-Z_][a-zA-Z0-9_]*/,

    literal: $ => choice(
      $.int_lit,
      $.float_lit,
      $.string_lit,
      $.char_lit,
      'true',
      'false'
    ),

    int_lit: $ => choice(
      '0',
      /[1-9][0-9]*/,
      /0x[0-9a-fA-F]+/,
      /0o[0-7]+/,
      /0b[01]+/
    ),

    float_lit: $ => /(0|[1-9][0-9]*)\.[0-9]+/,
    string_lit: $ => /"[^"]*"/,
    char_lit: $ => /'[^']'/,

    comment: $ => token(seq('//', /.*/)),
  }
});
