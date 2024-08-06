import os
import time

if __name__ == "__main__":
    print("cov_llm_proc called")
    os.system("cd ./llm_result_files && echo cov_llm_proc_called > cov_file.txt")
    time.sleep(3)