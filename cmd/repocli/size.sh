#!/bin/bash
###############################################################################
#
#  This script illustrates an example of using the repocli to get the size and 
#  total numnber of files in a remote (DR) directory.
#
###############################################################################  

REPOCLI_BIN=/usr/bin/repocli

# check if given line from `repocli ls -l` refers to a file
function isFile() {
    echo "$1" | grep "^-rw" >/dev/null 2>&1
}

# get size of a remote collection 
function size() {
    dir=$1
    local c=0
    while read line; do
    isFile "$line" &&
        c=$(( $c + $(echo "$line" | awk '{print $2}') )) ||
	c=$(( $c + $(size "$(echo "$line" | awk '{print $NF}')") ))
    done < <( $REPOCLI_BIN ls -l "${1}" | sed 's/^ *//g' )
    echo $c
}

# get total number of files of a remote collection 
function nof() {
    dir=$1
    local c=0
    while read line; do
        isFile "$line" &&
            c=$(( $c + 1 )) ||
            c=$(( $c + $(nof "$(echo "$line" | awk '{print $NF}')") ))
    done < <( $REPOCLI_BIN ls -l "${1}" | sed 's/^ *//g' )
    echo $c
}

# get size and the total number of files of a remote collection
function size_nof() {
    dir=$1
    local csize=0
    local cnof=0
    while read line; do
	isFile "$line"
        if [ $? -eq 0 ]; then
            csize=$(( $csize + $(echo "$line" | awk '{print $2}') ))
            cnof=$(( $cnof + 1 ))
        else
	    cdata=$(size_nof "$(echo "$line" | awk '{print $NF}')")
	    csize=$(( $csize + $(echo $cdata | awk '{print $1}') ))
	    cnof=$(( $cnof   + $(echo $cdata | awk '{print $2}') ))
        fi
    done < <( $REPOCLI_BIN ls -l "${1}" | sed 's/^ *//g' )
    echo $csize $cnof
}
 
[ $# -ne 1 ] && echo "usage: $0 {path}" >&2 && exit 1
size_nof "$1" | awk '{print "size:"$1" nof:"$2}'
