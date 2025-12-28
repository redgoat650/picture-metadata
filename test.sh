#!/bin/bash
# Quick test script for the picture-metadata tool

echo "Running dry run test on a small subset..."
echo "==========================================="

docker run --rm \
  -v ~/.ssh:/root/.ssh:ro \
  -v $(pwd)/output:/data/output \
  --add-host nas-photos:142.254.0.235 \
  picture-metadata:latest \
  -source "/var/services/homes/redgoat650/Photos/Jane Photos/Curated Photos/1949 and before/Dyson-Williams/Dysons" \
  -dest /data/output \
  -ssh-host redgoat650@nas-photos:69 \
  -dry-run

echo ""
echo "==========================================="
echo "Dry run complete! Review the output above."
echo "To run on the full collection, use:"
echo ""
echo "docker run --rm \\"
echo "  -v ~/.ssh:/root/.ssh:ro \\"
echo "  -v \$(pwd)/output:/data/output \\"
echo "  --add-host nas-photos:142.254.0.235 \\"
echo "  picture-metadata:latest \\"
echo "  -source \"/var/services/homes/redgoat650/Photos/Jane Photos/Curated Photos\" \\"
echo "  -dest /data/output \\"
echo "  -ssh-host redgoat650@nas-photos:69 \\"
echo "  -verbose"
