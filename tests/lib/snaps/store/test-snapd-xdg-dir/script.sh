#!/bin/bash
if [ -d "$XDG_RUNTIME_DIR" ];
then
    exit 0
else
    echo "The XDG_RUNTIME_DIR doesn't exist. Test failed."
    exit 255
fi
