#!/bin/sh

curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d @examples/chat/basic-cloud-request.json
