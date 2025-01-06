import os

os.system("sudo rm ./experiment_output_llmenabled.txt")
os.system("sudo rm ./cov_folder_vm_vm-* -rf")
os.system("sudo rm ./workdir/* -rf")
os.truncate("./close_cov_result.txt", 0)
