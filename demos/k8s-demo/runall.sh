#!/bin/bash

export CREATE_AND_CONFIGURE_LBS=true
green_seq=$(tput setaf 2)
normal_seq=$(tput sgr0)
green (){
    echo -e \\n${green_seq}"${1}"${normal_seq}
}

green "##### $(date) runall.sh #####"

for script in \
    01_create_db.sh \
    02_configure_db.sh \
    03_deploy_app.sh \
    04_test_deployment.sh
do
    green "***** ${script} *****"
    ./${script}
done

green "***** Rotating Password *****"
./rotate_password.sh "rotated"
green "***** 04_test_deployment.sh *****"
./04_test_deployment.sh

green "##### $(date) runall.sh complete #####"
