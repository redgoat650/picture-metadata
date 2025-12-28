#!/bin/bash
# Wrapper script to run picture-metadata in Docker

IMAGE_NAME="picture-metadata:latest"

# Build the image if it doesn't exist
if [[ "$(docker images -q $IMAGE_NAME 2> /dev/null)" == "" ]]; then
    echo "Building Docker image..."
    docker build -t $IMAGE_NAME .
fi

# Run the container with SSH keys/config and current directory mounted
docker run --rm \
    -v "$HOME/.ssh:/root/.ssh:ro" \
    -v "$(pwd):/data" \
    --add-host nas-photos:142.254.0.235 \
    $IMAGE_NAME "$@"
