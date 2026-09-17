# Use DaoCloud mirrored vllm image for China region for gpu with Volta、Turing、Ampere、Ada Lovelace、Hopper、Blackwell architecture (7.0 <= Compute Capability <= 12.1)
# The default base image uses vLLM 0.21.0 with CUDA 13.0. For CUDA 12.9 environments, switch to the commented cu129 image below.
# Compute Capability version query (https://developer.nvidia.com/cuda-gpus)
# support x86_64 architecture and ARM(AArch64) architecture
FROM docker.m.daocloud.io/vllm/vllm-openai:v0.21.0
# FROM docker.m.daocloud.io/vllm/vllm-openai:v0.21.0-cu129

# Install libgl for opencv support
RUN apt-get update && \
    apt-get install -y \
        libgl1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install MinerU 4 with Torch; reuse vLLM from the base image.
RUN python3 -m pip install -U 'mineru[torch]>=4.0,<5' -i https://mirrors.aliyun.com/pypi/simple --break-system-packages && \
    python3 -m pip cache purge

# Keep model downloads and runtime selection aligned, including starts without a GPU.
ENV MINERU_MODEL_SMALL_BACKEND=torch \
    MINERU_MODEL_VLM_ENGINE=vllm

# Synchronize models into the mounted cache on first startup, then reuse verified repositories.
ENTRYPOINT ["/bin/bash", "-c", "set -e; if ! mineru-kit models verify --tier standard --small-backend torch --vlm-engine vllm; then mineru-kit models download --tier standard --small-backend torch --vlm-engine vllm -s modelscope; fi; export MINERU_MODEL_SOURCE=local; exec \"$@\"", "--"]
