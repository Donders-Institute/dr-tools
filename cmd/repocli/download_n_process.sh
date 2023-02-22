#!/bin/bash
###############################################################################
#
#  This script illustrates an example of using the repocli to iterate over MRI
#  raw data of participants stored in a DR collection and convert the downloaded
#  data to the NIFTI format using dcm2niix.
#
###############################################################################  

# path at which the `repocli` binary is located
REPOCLI_BIN=/usr/bin/repocli

# path of DR collection
DR_COLL_DIR=/dccn/DAC_3010000.05_873

# path of project directory
PROJECT_DIR=/project/3010000.05

# loop over subject folders in the DR collection
for subdir in $($REPOCLI_BIN ls -l ${DR_COLL_DIR}/raw_bitcoin_tutorial | grep '^ d' | awk '{print $NF}'); do

    # loop over session folders in each subject folder
    for sesdir in $($REPOCLI_BIN ls -l "${subdir}" | grep '^ d' | awk '{print $NF}'); do
        
        # download session folder into the `wordir` sub-folder of the project directory
        dstdir=${PROJECT_DIR}/workdir/$(basename "${sesdir}")

        echo "Downloading $sesdir to $dstdir ..."
        $REPOCLI_BIN get -s "${sesdir}/" "${dstdir}"

        # process the data only if the download is completed successfully
        if [ $? -eq 0 ]; then
            echo "Processing downloaded data ${dstdir} ..."
            dcm2niix -o ${PROJECT_DIR}/nifti/$(basename "${subdir}")/$(basename "${sesdir}") -g y "${dstdir}"

            echo "Removing downloaded data ${dstdir} ..."
            rm -rf "${dstdir}"
        fi
    done
done
