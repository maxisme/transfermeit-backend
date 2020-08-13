#!/bin/bash
git pull
export $(grep -v '^#' .env | xargs)
docker stack deploy -c stack.yml transfmeit-backend

export TAG="develop"
export SUBDOM="$TAG."
docker stack deploy -c stack.yml dev-transfmeit-backend