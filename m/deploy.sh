#!/bin/bash

go build -ldflags="-s -w"
scp m server:~
ssh server sudo mv  m /usr/local/bin
