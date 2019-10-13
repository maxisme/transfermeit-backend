void setBuildStatus(String message, String state) {
  step([
      $class: "GitHubCommitStatusSetter",
      reposSource: [$class: "ManuallyEnteredRepositorySource", url: "https://github.com/maxisme/transfermeit-backend"],
      contextSource: [$class: "ManuallyEnteredCommitContextSource", context: "ci/jenkins/build-status"],
      errorHandlers: [[$class: "ChangingBuildStatusErrorHandler", result: "UNSTABLE"]],
      statusResultSource: [ $class: "ConditionalStatusResultSource", results: [[$class: "AnyBuildResult", message: message, state: state]] ]
  ]);
}

node() {
    try{
        checkout scm
        docker.image('mysql:5').withRun('-e "MYSQL_ROOT_PASSWORD=root"') { c ->
            docker.image('golang:1.12-alpine').withRun("--link ${c.id}:db -u root") {
                stage('Test'){
                    sh 'cd $WORKSPACE && session_key=UH9ax500yN4mnTO60WLY2ae943tsqzFw test_db_host="root:root@tcp(db:3306)" db="root:root@tcp(db:3306)/transfermeit_test" go test'
                }
            }
        }
        stage('Deploy'){
            sh 'ssh -o StrictHostKeyChecking=no jenk@sock.transferme.it "sudo /bin/bash /root/transfermeit-backend/deploy.sh"'
        }
        setBuildStatus("Build succeeded", "SUCCESS");
    } catch (e) {
        echo 'Err: ' + e.toString()
        currentBuild.result = "BROKEN"
        setBuildStatus("Build failed", "FAILURE");
    }

    deleteDir()
}