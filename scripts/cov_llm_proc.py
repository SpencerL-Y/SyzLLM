import os
import time
import sys

if __name__ == "__main__":
    current_file = sys.argv[1]
    print("cov_llm_proc called")
    path = os.path.join(current_file)
    opened_file = open(path, "r")
    mode = -1
    temp_call_name = ""
    temp_call_args = ""
    call_name_list = []
    call_args_str_list = []
    for line in opened_file.readlines():
        if line.startswith("-----"):
            # detecting and changing the mode
            if line.find("call") != -1:
                mode = 1
                temp_call_name = ""
                temp_call_args = ""
            elif line.find("args") != -1:
                mode = 2
            elif line.find("covers") != -1:
                call_name_list.append(temp_call_name)
                call_args_str_list.append(temp_call_args)
                mode = 3
            else:
                assert(False)
        else:
            assert(mode > 0)
            if mode == 1: 
                # call
                temp_call_name += line
            elif mode == 2:
                # arg
                temp_call_args += line
            elif mode == 3:
                # obtain cover and use addr2line
                os.system("addr2line " + line + " >> ./temp.txt")
                pass
            else:
                assert(False)

    os.system("cd ./llm_result_files && echo cov_llm_proc_called > cov_file.txt")