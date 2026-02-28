# 1. Delete the bad query files from Helix's runtime
rm -rf ~/.config/helix/runtime/queries/synovium/

# 2. Re-generate the C bindings locally
cd tree-sitter-synovium
npx tree-sitter generate
cd ..

# 3. Rebuild Helix's internal shared object
hx --grammar build
