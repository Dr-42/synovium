extern fn tree_sitter_tree_sitter_synovium() callconv(.c) *const anyopaque;

pub fn language() *const anyopaque {
    return tree_sitter_tree_sitter_synovium();
}
