#!/bin/bash
# cd /root/ && git clone https://github.com/maxisme/transfermeit-backend
# $ visudo
# jenk ALL = NOPASSWD: /bin/bash /root/transfermeit-backend/deploy.sh
# remember to add .env file

cd /root/transfermeit-backend/

git fetch &> /dev/null
diffs=$(git diff master origin/master)

if [ ! -z "$diffs" ]
then
    echo "Pulling code from GitHub..."
    git checkout master
    git pull origin master

    # update server
    docker-compose up --build -d

    # run db migrations
    docker-compose up flyway

    # kill all unused dockers
    docker system prune -f
else
    echo "Already up to date"
fi