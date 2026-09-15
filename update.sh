#!/bin/bash

set -e

echo "==> Checking repository..."
git status

echo
read -p "Commit message: " MESSAGE

if [ -z "$MESSAGE" ]; then
    echo "Error: Commit message cannot be empty."
    exit 1
fi

echo
echo "==> Staging changes..."
git add .

echo "==> Creating commit..."
git commit -m "$MESSAGE"

echo "==> Pushing..."
git push

echo
echo "==> Update complete!"
