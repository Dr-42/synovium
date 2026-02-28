
const PREC = {
  assign: 1,
  range: 2,
  logical_or: 3,
  logical_and: 4,
  bitwise_or: 5,
  bitwise_xor: 6,
  bitwise_and: 7,
  equality: 8,
  relational: 9,
  shift: 10,
  additive: 11,
  multiplicative: 12,
  cast: 13,
  unary: 14,
  call: 16,
  field: 17,
  index: 18,
  bubbling: 19,
  type_path: 20, // <-- Added highest precedence to greedily consume type namespaces
};

module.exports = grammar({
  name: 'synovium',

  extras: $ => [
    /\s/,
    $.comment,
  ],

  conflicts: $ => [
    [$.base_type],
    [$.primary_expr, $.struct_init_expr]
  ],

  rules: {
    source_file: $ => repeat($.declaration),

    comment: $ => /\/\/[^\n]*/,

    // --- 1. TOP LEVEL DECLARATIONS ---
    declaration: $ => choice(
      seq($.variable_decl, ';'),
      $.struct_decl,
      $.enum_decl,
      $.impl_decl,
      $.function_decl
    ),

    // --- 2. DATA STRUCTURES & IMPL ---
    struct_decl: $ => seq('struct', field('name', $.identifier), '{', optional($.field_decl_list), '}'),
    field_decl_list: $ => seq($.field_decl, repeat(seq(',', $.field_decl)), optional(',')),
    field_decl: $ => seq(field('name', $.identifier), ':', field('type', $.type)),

    enum_decl: $ => seq('enum', field('name', $.identifier), '{', optional($.variant_list), '}'),
    variant_list: $ => seq($.variant, repeat(seq(',', $.variant)), optional(',')),
    variant: $ => seq(field('name', $.identifier), optional(seq('(', $.type_list, ')'))),

    impl_decl: $ => seq('impl', field('name', $.identifier), '{', repeat($.function_decl), '}'),

    // --- 3. FUNCTIONS & VARIABLES ---
    function_decl: $ => seq(
      'fnc',
      optional(field('name', $.identifier)),
      '(', optional($.parameter_list), ')',
      optional(seq($.return_op, field('return_type', $.type))),
      field('body', $.block)
    ),
    parameter_list: $ => seq($.parameter, repeat(seq(',', $.parameter)), optional(',')),
    parameter: $ => seq(field('name', $.identifier), ':', field('type', $.type)),
    return_op: $ => choice(':=', '='),

    variable_decl: $ => seq(
      field('name', $.identifier),
      ':',
      field('type', $.type),
      field('operator', $.assign_op),
      field('value', $.expression)
    ),
    assign_op: $ => choice('=', '~=', ':=', '+=', '-=', '*=', '/=', '%='),

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
    base_type: $ => choice(
      $.type_identifier,
      seq('[', $.type, ';', choice($.expression, ':'), ']'),
      seq('fnc', '(', optional($.type_list), ')', optional(seq($.return_op, $.type)))
    ),
    
    // <-- Wrapped in prec.left with type_path precedence to force shifting the '.'
    type_identifier: $ => prec.left(PREC.type_path, seq($.identifier, repeat(seq('.', $.identifier)))),
    
    type_list: $ => seq($.type, repeat(seq(',', $.type))),

    // --- 6. EXPRESSIONS ---
    expression: $ => choice(
      $.primary_expr,
      $.unary_expr,
      $.binary_expr,
      $.assignment_expr,
      $.range_expr,
      $.cast_expr,
      $.call_expr,
      $.field_access_expr,
      $.index_expr,
      $.bubbling_expr
    ),

    assignment_expr: $ => prec.right(PREC.assign, seq($.expression, $.assign_op, $.expression)),
    range_expr: $ => prec.left(PREC.range, seq($.expression, '...', $.expression)),
    
    binary_expr: $ => choice(
      prec.left(PREC.logical_or, seq($.expression, '||', $.expression)),
      prec.left(PREC.logical_and, seq($.expression, '&&', $.expression)),
      prec.left(PREC.bitwise_or, seq($.expression, '|', $.expression)),
      prec.left(PREC.bitwise_xor, seq($.expression, '^', $.expression)),
      prec.left(PREC.bitwise_and, seq($.expression, '&', $.expression)),
      prec.left(PREC.equality, seq($.expression, choice('==', '!='), $.expression)),
      prec.left(PREC.relational, seq($.expression, choice('<', '<=', '>', '>='), $.expression)),
      prec.left(PREC.shift, seq($.expression, choice('<<', '>>'), $.expression)),
      prec.left(PREC.additive, seq($.expression, choice('+', '-'), $.expression)),
      prec.left(PREC.multiplicative, seq($.expression, choice('*', '/', '%'), $.expression)),
    ),

    cast_expr: $ => prec.left(PREC.cast, seq($.expression, 'as', $.type)),
    unary_expr: $ => prec.right(PREC.unary, seq(choice('!', '~', '-', '*', '&'), $.expression)),

    call_expr: $ => prec.left(PREC.call, seq(field('function', $.expression), '(', optional($.argument_list), ')')),
    field_access_expr: $ => prec.left(PREC.field, seq(field('object', $.expression), '.', field('field', $.identifier))),
    index_expr: $ => prec.left(PREC.index, seq(field('object', $.expression), '[', choice($.expression, seq($.expression, '...', $.expression), ':'), ']')),
    bubbling_expr: $ => prec.left(PREC.bubbling, seq($.expression, '?')),

    // --- 7. PRIMARY EXPRESSIONS & CONTROL FLOW ---
    primary_expr: $ => choice(
      $.identifier,
      $.literal,
      seq('(', $.expression, ')'),
      $.struct_init_expr,
      $.if_expr,
      $.match_expr,
      $.loop_expr,
      $.block,
      $.lambda_expr
    ),

    lambda_expr: $ => seq(
      'fnc',
      optional(field('name', $.identifier)),
      '(', optional($.parameter_list), ')',
      optional(seq($.return_op, field('return_type', $.type))),
      field('body', $.block)
    ),

    struct_init_expr: $ => seq(field('name', $.identifier), '{', optional($.struct_init_list), '}'),
    struct_init_list: $ => seq($.struct_init_field, repeat(seq(',', $.struct_init_field)), optional(',')),
    struct_init_field: $ => seq('.', field('name', $.identifier), '=', field('value', $.expression)),

    if_expr: $ => seq(
      'if', field('condition', $.expression), field('consequence', $.block),
      repeat(seq('elif', field('elif_condition', $.expression), field('elif_consequence', $.block))),
      optional(seq('else', field('alternative', $.block)))
    ),

    match_expr: $ => seq('match', field('value', $.expression), '{', repeat($.match_arm), '}'),
    match_arm: $ => seq(field('pattern', $.pattern), '->', field('body', $.block), optional(',')),
    pattern: $ => seq($.identifier, repeat(seq('.', $.identifier)), optional(seq('(', optional($.identifier_list), ')'))),

    loop_expr: $ => seq('loop', optional(seq('(', field('condition', $.loop_cond), ')')), field('body', $.block)),
    loop_cond: $ => choice($.variable_decl_no_semi, $.expression),
    variable_decl_no_semi: $ => seq(field('name', $.identifier), ':', field('type', $.type), field('operator', $.assign_op), field('value', $.expression)),

    argument_list: $ => seq($.expression, repeat(seq(',', $.expression)), optional(',')),
    identifier_list: $ => seq($.identifier, repeat(seq(',', $.identifier)), optional(',')),

    // --- 8. TERMINALS (Lexicon) ---
    identifier: $ => /[a-zA-Z_][a-zA-Z0-9_]*/,
    int_lit: $ => choice('0', /[1-9][0-9]*/, /0x[0-9a-fA-F]+/, /0o[0-7]+/, /0b[01]+/),
    float_lit: $ => /(0|[1-9][0-9]*)\.[0-9]+/,
    string_lit: $ => /"[^"]*"/,
    char_lit: $ => /'[^']'/,
    literal: $ => choice($.int_lit, $.float_lit, $.string_lit, $.char_lit, 'true', 'false'),
  }
});
