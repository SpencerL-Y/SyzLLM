import os
import time
import sys

if __name__ == "__main__":
    current_file = sys.argv[1]
    print("cov_llm_proc called")
    path = os.path.join("./llm_result_files/", current_file)
    opened_file = open(path, "r")
    mode = -1
    description_of_call = ""
    for line in opened_file.readlines():
        if line.startswith("-----"):
            # detecting and changing the mode
            if line.find("call") != -1:
                mode = 1
            elif line.find("arg") != -1:
                mode = 2
            elif line.find("cover") != -1:
                mode = 3
            else:
                assert(False)
        else:
            assert(mode > 0)
            if mode == 1: 
                # call
                pass
            elif mode == 2:
                # arg
                pass
            elif mode == 3:
                # obtain cover and use addr2line
                pass
            else:
                assert(False)

    os.system("cd ./llm_result_files && echo cov_llm_proc_called > cov_file.txt")
    time.sleep(3)