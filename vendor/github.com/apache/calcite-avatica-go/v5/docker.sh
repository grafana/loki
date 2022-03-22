#!/bin/bash

# Licensed to the Apache Software Foundation (ASF) under one or more
# contributor license agreements.  See the NOTICE file distributed with
# this work for additional information regarding copyright ownership.
# The ASF licenses this file to you under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance with
# the License.  You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

GITBOX_URL=https://gitbox.apache.org/repos/asf/calcite-avatica-go.git
RELEASE_REPO=https://dist.apache.org/repos/dist/release/calcite/
DEV_REPO=https://dist.apache.org/repos/dist/dev/calcite/
PRODUCT=apache-calcite-avatica-go

function terminate() {
    printf "\n\nUser terminated build. Exiting...\n"
    exit 1
}

trap terminate SIGINT

init_release(){
    apk --no-cache add git gnupg tar
}

init_upload(){
    apk --no-cache add git subversion
}

KEYS=()

GPG_COMMAND="gpg"

get_gpg_keys (){
    GPG_KEYS=$($GPG_COMMAND --list-keys --with-colons --keyid-format LONG)

    KEY_NUM=1

    KEY_DETAILS=""

    while read -r line; do

        IFS=':' read -ra PART <<< "$line"

        if [ ${PART[0]} == "pub" ]; then

            if [ -n "$KEY_DETAILS" ]; then
                KEYS[$KEY_NUM]=$KEY_DETAILS
                KEY_DETAILS=""
                ((KEY_NUM++))

            fi

            KEY_DETAILS=${PART[4]}
        fi

        if [ ${PART[0]} == "uid" ]; then
            KEY_DETAILS="$KEY_DETAILS - ${PART[9]}"
        fi

    done <<< "$GPG_KEYS"

    if [ -n "$KEY_DETAILS" ]; then
        KEYS[$KEY_NUM]=$KEY_DETAILS
    fi
}

mount_gpg_keys(){
    mkdir -p /.gnupg

    if [[ -z "$(ls -A /.gnupg)" ]]; then
        echo "Please mount the contents of your .gnupg folder into /.gnupg. Exiting..."
        exit 1
    fi

    mkdir -p /root/.gnupg

    cp -r /.gnupg/ /root/

    chmod -R 700 /root/.gnupg/

    rm -rf /root/.gnupg/*.lock
}

SELECTED_GPG_KEY=""

select_gpg_key(){

    get_gpg_keys

    export GPG_TTY=/dev/console

    touch /root/.gnupg/gpg-agent.conf
    echo 'default-cache-ttl 10000' >> /root/.gnupg/gpg-agent.conf
    echo 'max-cache-ttl 10000' >> /root/.gnupg/gpg-agent.conf

    echo "Starting GPG agent..."
    gpg-agent --daemon

    while $INVALID_KEY_SELECTED; do

        if [[ "${#KEYS[@]}" -le 0 ]]; then
            echo "You do not have any GPG keys available. Exiting..."
            exit 1
        fi

        echo "You have the following GPG keys:"

        for i in "${!KEYS[@]}"; do
                echo "$i) ${KEYS[$i]}"
        done

        read -p "Select your GPG key for signing: " KEY_INDEX

        SELECTED_GPG_KEY=$(sed 's/ -.*//' <<< ${KEYS[$KEY_INDEX]})

        if [[ -z $SELECTED_GPG_KEY ]]; then
            echo "Selected key is invalid, please try again."
            continue
        fi

        echo "Authenticating your GPG key..."

        echo "test" | $GPG_COMMAND --local-user $SELECTED_GPG_KEY --output /dev/null --sign -

        if [[ $? != 0 ]]; then
            echo "Invalid GPG passphrase or GPG error. Please try again."
            continue
        fi

        echo "You have selected the following GPG key to sign the release:"
        echo "${KEYS[$KEY_INDEX]}"

        INVALID_CONFIRMATION=true

        while $INVALID_CONFIRMATION; do
            read -p "Is this correct? (y/n) " CONFIRM

            if [[ ($CONFIRM == "Y") || ($CONFIRM == "y") ]]; then
                INVALID_KEY_SELECTED=false
                INVALID_CONFIRMATION=false
            elif [[ ($CONFIRM == "N") || ($CONFIRM == "n") ]]; then
                INVALID_CONFIRMATION=false
            fi
        done
    done
}

check_release_guidelines(){

    # Exclude files without the Apache license header
    for i in $(git ls-files); do
       case "$i" in
       # The following are excluded from the license header check

       # License files
       (LICENSE|NOTICE);;

       # Generated files
       (message/common.pb.go|message/requests.pb.go|message/responses.pb.go|Gopkg.lock|Gopkg.toml|go.mod|go.sum);;

       # Binaries
       (test-fixtures/calcite.png);;

       (*) grep -q "Licensed to the Apache Software Foundation" $i || echo "$i has no header";;
       esac
    done

    # Check copyright year in NOTICE
    if ! grep -Fq "Copyright 2012-$(date +%Y)" NOTICE; then
        echo "Ending copyright year in NOTICE is not $(date +%Y)"
        exit 1
    fi
}

check_if_tag_exists(){
    # Get new tags from remote
    git fetch --tags $GITBOX_URL

    for tag in "$@"
    do
        # Check to see if a tag with a v in front of it has been released
        if  git show-ref --tags | egrep -q "refs/tags/v$tag$"; then
            echo "A release with version $1 was already released. Check that the version number entered is correct."
            exit 1
        fi

        # Check to see if a tag without a v in front of it has been released (3.0.0 and below)
        if  git show-ref --tags | egrep -q "refs/tags/$tag$"; then
            echo "A release with version $1 was already released. Check that the version number entered is correct."
            exit 1
        fi
    done
}

check_local_remote_are_even(){
    REMOTE_COMMIT=$(git ls-remote $GITBOX_URL | head -1 | sed "s/[[:space:]]HEAD//")
    LOCAL_COMMIT=$(git rev-parse master)

    if [[ $REMOTE_COMMIT != $LOCAL_COMMIT ]]; then
        echo "Master in Apache repository is not even with local master"
        exit 1
    fi
}


check_import_paths(){
    TAG_MAJOR_VERSION=$(echo $1 | sed -e 's/\..*//')

    # Check that go.mod's module path contains the right version
    if ! grep -Fq "module github.com/apache/calcite-avatica-go/$TAG_MAJOR_VERSION" go.mod; then
        echo "Module declaration in go.mod does not contain the correct version. Expected: $TAG_MAJOR_VERSION"
        exit 1
    fi

    # Make sure import paths contain the correct version
    BAD_IMPORT_PATHS=false

    for i in $(git ls-files); do

        if [[ "$i" == "docker.sh" || "$i" =~ \.md$ ]]; then
            continue
        fi

        lines=$(grep -F -s '"github.com/apache/calcite-avatica-go' $i) || true

        if ! [[ -z "$lines" ]]; then
            while read -r line; do
                if ! grep -q "github.com/apache/calcite-avatica-go/$TAG_MAJOR_VERSION" <<< "$line" ; then
                    BAD_IMPORT_PATHS=true
                    echo "Import for github.com/apache/calcite-avatica-go in $i does not have the correct version ($TAG_MAJOR_VERSION) in its path"
                fi
            done <<< "$lines"
        fi
    done

    if "$BAD_IMPORT_PATHS" == true; then
        exit 1
    fi
}

RELEASE_VERSION=""
RC_NUMBER=""
ASF_USERNAME=""
ASF_PASSWORD=""

set_git_credentials(){
    echo https://$ASF_USERNAME:$ASF_PASSWORD@gitbox.apache.org >> /root/.git-credentials
    git config --global credential.helper 'store --file=/root/.git-credentials'
}

get_asf_credentials(){
    while $ASF_CREDS_NOT_CONFIRMED; do
        read -p "Enter your ASF username: " ASF_USERNAME
        read -s -p "Enter your ASF password: " ASF_PASSWORD
        printf "\n"
        echo "Your ASF Username is:" $ASF_USERNAME

        INVALID_CONFIRMATION=true

        while $INVALID_CONFIRMATION; do
            read -p "Is this correct? (y/n) " CONFIRM

            if [[ ($CONFIRM == "Y") || ($CONFIRM == "y") ]]; then
                ASF_CREDS_NOT_CONFIRMED=false
                INVALID_CONFIRMATION=false
            elif [[ ($CONFIRM == "N") || ($CONFIRM == "n") ]]; then
                INVALID_CONFIRMATION=false
            fi
        done
    done
}

clean_release_directory(){
    rm -rf dist
}

make_release_artifacts(){

    CURRENT_BRANCH=$(git branch | grep \* | cut -d ' ' -f2)

    if [ $CURRENT_BRANCH != "master" ]; then
        echo "You are currently on the $CURRENT_BRANCH branch. A release must be made from the master branch. Exiting..."
        exit 1
    fi

    check_local_remote_are_even

    check_release_guidelines

    while $NOT_CONFIRMED; do
        read -p "Enter the version number to be released (example: 4.0.0, do not include the rc number): " RELEASE_VERSION
        read -p "Enter the release candidate number (example: if you are releasing rc0, enter 0): " RC_NUMBER
        echo "Build configured as follows:"
        echo "Release: $RELEASE_VERSION-rc$RC_NUMBER"

        INVALID_CONFIRMATION=true

        while $INVALID_CONFIRMATION; do
            read -p "Is this correct? (y/n) " CONFIRM

            if [[ ($CONFIRM == "Y") || ($CONFIRM == "y") ]]; then
                NOT_CONFIRMED=false
                INVALID_CONFIRMATION=false
            elif [[ ($CONFIRM == "N") || ($CONFIRM == "n") ]]; then
                INVALID_CONFIRMATION=false
            fi
        done
    done

    select_gpg_key

    check_if_tag_exists $RELEASE_VERSION $RELEASE_VERSION-rc$RC_NUMBER

    TAG="v$RELEASE_VERSION-rc$RC_NUMBER"

    check_import_paths $TAG

    clean_release_directory

    TAR_FILE=$PRODUCT-$RELEASE_VERSION-src.tar.gz
    RELEASE_DIR=dist/$PRODUCT-$RELEASE_VERSION-rc$RC_NUMBER

    #Make release dir
    mkdir -p $RELEASE_DIR

    # Make tar
    tar -zcf $RELEASE_DIR/$TAR_FILE --transform "s/^/$PRODUCT-$RELEASE_VERSION-src\//g" $(git ls-files)

    cd $RELEASE_DIR

    # Calculate SHA512
    gpg --print-md SHA512 $TAR_FILE > $TAR_FILE.sha512

    # Sign
    gpg -u $SELECTED_GPG_KEY --armor --output $TAR_FILE.asc --detach-sig $TAR_FILE

    echo "Release artifacts created!"
}

make_release_artifacts_and_push_tag(){
    make_release_artifacts

    get_asf_credentials

    # Create the tag
    git tag $TAG

    # Push the tag
    set_git_credentials

    git push $GITBOX_URL $TAG

    echo "Release $RELEASE_VERSION-rc$RC_NUMBER has been tagged and pushed!"
}

publish_release_for_voting(){

    LATEST_TAG=$(git describe --tags `git rev-list --tags --max-count=1`)

    if [[ ! $LATEST_TAG =~ .+-rc[[:digit:]]+$ ]]; then
        echo "The latest tag ($LATEST_TAG) is not a RC release and should not be published for voting."
        exit 1
    fi

    TAG_WITHOUT_V=$(echo $LATEST_TAG | sed -e 's/v//')
    TAG_WITHOUT_RC=$(echo $TAG_WITHOUT_V | sed -e 's/-rc[0-9][0-9]*//')
    SOURCE_RELEASE=$PRODUCT-$TAG_WITHOUT_RC-src.tar.gz
    GPG_SIGNATURE=$PRODUCT-$TAG_WITHOUT_RC-src.tar.gz.asc
    SHA512=$PRODUCT-$TAG_WITHOUT_RC-src.tar.gz.sha512
    COMMIT=$(git rev-list -n 1 $LATEST_TAG)

    # Check to see a release is built
    MISSING_FILES=false

    if [ ! -f "dist/$PRODUCT-$TAG_WITHOUT_V/$SOURCE_RELEASE" ]; then
        echo "Did not find source release ($SOURCE_RELEASE) in dist folder."
        MISSING_FILES=true
    fi

    if [ ! -f "dist/$PRODUCT-$TAG_WITHOUT_V/$GPG_SIGNATURE" ]; then
        echo "Did not find GPG signature ($GPG_SIGNATURE) in dist folder."
        MISSING_FILES=true
    fi

    if [ ! -f "dist/$PRODUCT-$TAG_WITHOUT_V/$SHA512" ]; then
        echo "Did not find SHA512 ($SHA512) in dist folder."
        MISSING_FILES=true
    fi

    if $MISSING_FILES == true; then
        exit 1
    fi

    while $NOT_CONFIRMED; do
        echo "Publish configured as follows:"
        echo "Release: $TAG_WITHOUT_V"

        INVALID_CONFIRMATION=true

        while $INVALID_CONFIRMATION; do
            read -p "Is this correct? (y/n) " CONFIRM

            if [[ ($CONFIRM == "Y") || ($CONFIRM == "y") ]]; then
                NOT_CONFIRMED=false
                INVALID_CONFIRMATION=false
            elif [[ ($CONFIRM == "N") || ($CONFIRM == "n") ]]; then
                INVALID_CONFIRMATION=false
            fi
        done
    done

    HASH_CONTENTS=$(cat "dist/$PRODUCT-$TAG_WITHOUT_V/$SHA512" | tr -d '\n')
    HASH=${HASH_CONTENTS#"$SOURCE_RELEASE: "}

    get_asf_credentials

    svn checkout $DEV_REPO /tmp/calcite --depth empty
    cp -R dist/$PRODUCT-$TAG_WITHOUT_V /tmp/calcite/

    cd /tmp/calcite
    svn add $PRODUCT-$TAG_WITHOUT_V
    chmod -x $PRODUCT-$TAG_WITHOUT_V/*

    svn commit -m "$PRODUCT-$TAG_WITHOUT_V" --force-log --username $ASF_USERNAME --password $ASF_PASSWORD

    [[ $LATEST_TAG =~ -rc([[:digit:]]+)$ ]]
    RC_NUMBER=${BASH_REMATCH[1]}

    read -p "Please enter your first name for the voting email: " FIRST_NAME

    echo "The release $PRODUCT-$TAG_WITHOUT_V has been uploaded to the development repository."
    printf "\n"
    printf "\n"
    echo "Email the following message to dev@calcite.apache.org. Please check the message before sending."
    printf "\n"
    echo "To: dev@calcite.apache.org"
    echo "Subject: [VOTE] Release $PRODUCT-$TAG_WITHOUT_RC (release candidate $RC_NUMBER)"
    echo "Message:
Hi all,

I have created a release for Apache Calcite Avatica Go $TAG_WITHOUT_RC, release candidate $RC_NUMBER.

Thanks to everyone who has contributed to this release. The release notes are available here:
https://github.com/apache/calcite-avatica-go/blob/$COMMIT/site/_docs/go_history.md

The commit to be voted on:
https://gitbox.apache.org/repos/asf?p=calcite-avatica-go.git;a=commit;h=$COMMIT

The hash is $COMMIT

The artifacts to be voted on are located here:
$DEV_REPO$PRODUCT-$TAG_WITHOUT_V/

The hashes of the artifacts are as follows:
src.tar.gz $HASH

Release artifacts are signed with the following key:
https://people.apache.org/keys/committer/$ASF_USERNAME.asc

Instructions for running the test suite is located here:
https://github.com/apache/calcite-avatica-go/blob/$COMMIT/site/develop/avatica-go.md#testing

Please vote on releasing this package as Apache Calcite Avatica Go $TAG_WITHOUT_RC.

To run the tests without a Go environment, install docker and docker-compose. Then, in the root of the release's directory, run: docker-compose run test

When the test suite completes, run \"docker-compose down\" to remove and shutdown all the containers.

The vote is open for the next 72 hours and passes if a majority of at least three +1 PMC votes are cast.

[ ] +1 Release this package as Apache Calcite Avatica Go $TAG_WITHOUT_RC
[ ]  0 I don't feel strongly about it, but I'm okay with the release
[ ] -1 Do not release this package because...


Here is my vote:

+1 (binding)

$FIRST_NAME
"
}

promote_release(){
    LATEST_TAG=$(git describe --tags `git rev-list --tags --max-count=1`)

    if [[ ! $LATEST_TAG =~ .+-rc[[:digit:]]+$ ]]; then
        echo "The latest tag ($LATEST_TAG) is not a RC release and should not be re-released."
        exit 1
    fi

    TAG_WITHOUT_V=$(echo $LATEST_TAG | sed -e 's/v//')
    TAG_WITHOUT_RC=$(echo $TAG_WITHOUT_V | sed -e 's/-rc[0-9][0-9]*//')

    if ! svn ls $DEV_REPO/$PRODUCT-$TAG_WITHOUT_V; then
        echo "The release $PRODUCT-$TAG_WITHOUT_V was not found in the dev repository. Was it uploaded for voting?"
        exit 1
    fi

    get_asf_credentials

    set_git_credentials

    git tag v$TAG_WITHOUT_RC $LATEST_TAG

    git push $GITBOX_URL v$TAG_WITHOUT_RC

    svn checkout $RELEASE_REPO /tmp/release
    rm -rf /tmp/release/$PRODUCT-$TAG_WITHOUT_RC
    mkdir -p /tmp/release/$PRODUCT-$TAG_WITHOUT_RC

    svn checkout $DEV_REPO/$PRODUCT-$TAG_WITHOUT_V /tmp/rc
    cp -rp /tmp/rc/* /tmp/release/$PRODUCT-$TAG_WITHOUT_RC

    cd /tmp/release

    svn add $PRODUCT-$TAG_WITHOUT_RC

    # If there is more than 1 release, delete all of them, except for the newest one
    # To do this, we do the following:
    # 1. Get the list of releases with verbose information from svn
    # 2. Sort by the first field (revision number) in descending order
    # 3. Select apache-calcite-avatica-go releases
    # 4. Exclude the release we're trying to promote, in case it was from a failed promotion.
    # 5. Trim all whitespace down to 1 empty space.
    # 6. Select field 7, which is each release's folder
    CURRENT_RELEASES=$(svn ls -v $RELEASE_REPO | sort -k1 -r | grep $PRODUCT | grep -v $PRODUCT-$TAG_WITHOUT_RC | tr -s ' ' | cut -d ' ' -f 7)

    RELEASE_COUNT=0
    while read -r RELEASE; do
        if [[ $RELEASE_COUNT -ne 0 ]]; then
            svn rm $RELEASE
            echo "Removing release $RELEASE"
        fi

        RELEASE_COUNT=$((RELEASE_COUNT+1))
    done <<< "$CURRENT_RELEASES"

    svn commit -m "Release $PRODUCT-$TAG_WITHOUT_RC" --force-log --username $ASF_USERNAME --password $ASF_PASSWORD

    echo "Release $PRODUCT-$LATEST_TAG successfully promoted to $PRODUCT-$TAG_WITHOUT_RC"
}

compile_protobuf(){

    if [ -z "$AVATICA_VERSION" ]; then
        echo "The AVATICA_VERSION environment variable must be set to a valid avatica version"
    fi

    if [ -z "$PROTOBUF_VERSION" ]; then
        echo "The PROTOBUF_VERSION environment variable must be set to a valid protobuf version"
    fi

    if [ -z "$GLIBC_VERSION" ]; then
        echo "The GLIBC_VERSION environment variable must be set to a valid protobuf version"
    fi

    apk --no-cache add ca-certificates git wget

    # Install glibc
    wget -q -O /etc/apk/keys/sgerrand.rsa.pub https://alpine-pkgs.sgerrand.com/sgerrand.rsa.pub
    wget -q -O /tmp/glibc.apk https://github.com/sgerrand/alpine-pkg-glibc/releases/download/${GLIBC_VERSION}/glibc-${GLIBC_VERSION}.apk
    apk add /tmp/glibc.apk


    # Install go protobuf compiler
    go install github.com/golang/protobuf/protoc-gen-go

    # Install protoc
    mkdir -p /tmp/protoc
    wget -q -O /tmp/protoc/protoc.zip https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOBUF_VERSION}/protoc-${PROTOBUF_VERSION}-linux-x86_64.zip
    cd /tmp/protoc
    unzip protoc.zip
    mv /tmp/protoc/include /usr/local/include
    mv /tmp/protoc/bin/protoc /usr/local/bin/protoc

    # Clone a temp copy of avatica and check out the selected version
    mkdir /tmp/avatica
    cd /tmp/avatica
    git init
    git remote add origin https://github.com/apache/calcite-avatica.git
    git config core.sparsecheckout true
    echo "core/src/main/protobuf/*" >> .git/info/sparse-checkout

    git fetch --depth=1 origin rel/avatica-$AVATICA_VERSION
    git checkout FETCH_HEAD

    # Compile the protobuf
    protoc --proto_path=/tmp/avatica/core/src/main/protobuf --go_out=import_path=message:/source/message/ /tmp/avatica/core/src/main/protobuf/*.proto

    echo "Protobuf compiled successfully"
}

case $1 in
    dry-run)
        init_release
        mount_gpg_keys
        make_release_artifacts
        ;;

    release)
        init_release
        mount_gpg_keys
        make_release_artifacts_and_push_tag
        ;;

    clean)
        clean_release_directory
        echo "Release directory ./dist removed"
        ;;

    publish-release-for-voting)
        init_upload
        publish_release_for_voting
        ;;

    promote-release)
        init_upload
        promote_release
        ;;

    compile-protobuf)
        compile_protobuf
        ;;

    *)
       echo $"Usage: $0 {dry-run|release|clean|publish-release-for-voting|promote-release}"
       ;;

esac

# End