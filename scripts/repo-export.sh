#!/bin/bash

PDBUTIL=$(pwd)/pdbutil
PDBUTIL_CFG=$(pwd)/config.yml

VIEWERDB=$(pwd)/.viewers
UMAP=$(pwd)/.viewers/umap

# Get email of a DR user account.
function dr_user_get_email() {
    iquest "%s" "select META_USER_ATTR_VALUE where USER_NAME = '$1' and META_USER_ATTR_NAME = 'email'" | grep -v CAT_NO_ROWS_FOUND
}

# Find DCCN user account id by matching email.
function pdb_uid_by_email() {
    ${PDBUTIL} user find $1 -c ${PDBUTIL_CFG} 2>/dev/null
}

# Initialize a git repo as a internal viewer database.
function init_viewer_db() {
    [ ! -d ${1}/.git ] && mkdir -p $1 && git init $1

    # get in repo and set username and email to suppress git messages
    cd $1 &&
      git config user.name irods &&
      git config user.email irods@irods-resc01.dccn.nl &&

    # get back to original directory before leaving this function
    cd -
}

# Resolve DR account to DCCN account mapping via email address.
# It also updates the local $UMAP file so that already mapped
# account can be reused.
function get_umap() {

    u=$1

    # try fetch the map from $UMAP file
    cat $UMAP | grep ${u} && return 0

    # try resolve the map by email
    email=$( dr_user_get_email $u ) &&
      uid=$( pdb_uid_by_email $email ) && id $uid || return 1

    echo "$u,${email,,},$uid" | tee -a $UMAP
}

# Add new viewers as DCCN local accounts to a locally exported dataset.
function add_viewers() {
    ds=$1
    n=$(basename $ds)

    acl=""
    ulist=()
    for u in $( git diff ${n} | grep '^+[a-zA-Z0-9]' | sed 's/^+//' ); do
        m=$( get_umap $u )
        if [ $? -eq 0 ]; then
            uid=${m##*,}
            [ ! -z ${uid} ] && acl="${uid}:r-x,${acl}" && ulist=("${ulist[@]}" ${u})
        else
            echo "fail to resolve DCCN user id: $u" >&2
        fi
    done

    if [ ! -z ${acl} ]; then
        setfacl -m $acl $ds && echo "${ulist[@]}" || return 1
    fi
}

# Remove existing viewers as DCCN local accounts to a locally exported dataset.
function del_viewers() {
    ds=$1
    n=$(basename $ds)
    acl=""
    ulist=()
    for u in $( git diff ${n} | grep '^-[a-zA-Z0-9]' | sed 's/^-//' ); do
        m=$( get_umap $u )
        if [ $? -eq 0 ]; then
            uid=${m##*,}
            [ ! -z ${uid} ] && acl="${uid},${acl}" && ulist=("${ulist[@]}" ${u})
        else
            echo "fail to resolve DCCN user id: $u" >&2
        fi
    done

    if [ ! -z ${acl} ]; then
        setfacl -x $acl $ds && echo "${ulist[@]}" || return 0
    fi
}

# Remove lines in file $1 from file $2.
function remove_lines() {
  f1="$1"
  f2="$2"
  tmp_file="$(mktemp)"
  grep -Fvxf "$f1" "$f2" > "$tmp_file"
  mv "$tmp_file" "$f2"
}

## MAIN PROGRAM STARTS HERE ##
cd $VIEWERDB

# initialize viewer database managed by git
init_viewer_db $VIEWERDB || exit 1

# generate empty umap file if it doesn't exist
[ ! -f $UMAP ] && touch $UMAP

# perform operation within the VIEWERDB
cd $VIEWERDB

# first iteration to dump all viewers of exported collections. Those viewers are
# DR users in one of the collection roles: manager, contributor, viewerByDUA, and
# viewerByManager.
for ds in $( ls -d /.repo/dccn/*:v[1-9] | sort ); do
    n=$(basename $ds)
    [ ! -L /cephfs/data/export/dccn/${n} ] && ln -s $ds /cephfs/data/export/dccn/${n} || echo "$ds already exported" >&2

    # load viewer lists currently implemented as local ACL
    [ ! -f ${n} ] && touch ${n} && git add ${n} && git commit -m "new ${n}"

    # get viewers as DR accounts
    iquest --no-page "%s" "select META_COLL_ATTR_VALUE where COLL_NAME = '/nl.ru.donders/di/dccn/${n%%:v*}' and META_COLL_ATTR_NAME in ('manager','contributor','viewerByManager','viewerByDUA')" | grep -v CAT_NO_ROWS_FOUND | sort > ${n}
done

# second iteration to map DR accounts of viewers to DCCN accounts; while adding
# them into the ACL of the exported filesystem (CephFS in this case).
for ds in $( ls -d /.repo/dccn/*:v[1-9] | sort ); do
    uadded=$(add_viewers $ds) || echo "fail adding viewers to $ds" >&2
    udeled=$(del_viewers $ds) || echo "fail deleting viewers from $ds" >&2

    # rollback to last update
    n=$(basename $ds)
    git checkout ${n}

    # update viewer db for added users
    for u in ${uadded}; do
        echo ${u} >> ${n}
    done

    # update viewer db for deleted users
    ftmp=$(mktemp)
    echo ${udeled} | sed 's/\s/\n/g' > ${ftmp}
    remove_lines ${ftmp} ${n}
    rm -f ${ftmp}

    # commit update or rollback
    git add ${n}
done

git commit -m "update: $(date +%Y-%m-%dT%H:%M:%S)"

# get back to original directory
cd -
