#!/bin/sh

# Gather all .syn files and run ```go run ../main.go <file>```

for f in *.syn; do
    echo "Running $f"
    go run ../main.go $f
    # Wait for a keypress
    read -n 1 -s -r -p "Press any key to continue..."
done
