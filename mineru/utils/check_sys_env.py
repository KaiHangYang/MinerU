# Copyright (c) Opendatalab. All rights reserved.
import platform
import shutil
import subprocess

from packaging import version


def is_windows_environment() -> bool:
    return platform.system() == "Windows"


# Detect if the current environment is a Mac computer
def is_mac_environment() -> bool:
    return platform.system() == "Darwin"


def is_linux_environment() -> bool:
    return platform.system() == "Linux"


# Detect if CPU is Apple Silicon architecture
def is_apple_silicon_cpu() -> bool:
    return platform.machine() in ["arm64", "aarch64"]


# If Mac computer with Apple Silicon architecture, check if macOS version is 13.5 or above
def is_mac_os_version_supported(min_version: str = "13.5") -> bool:
    if not is_mac_environment() or not is_apple_silicon_cpu():
        return False
    mac_version = platform.mac_ver()[0]
    if not mac_version:
        return False
    # print("Mac OS Version:", mac_version)
    return version.parse(mac_version) >= version.parse(min_version)

def check_nvidia_gpu_health(timeout: float = 5.0) -> tuple[bool, str | None]:
    """通过 nvidia-smi 探测宿主 GPU 及驱动是否正常。

    容器内 GPU 驱动崩溃（如 Xid 错误）后，已初始化的 torch.cuda 状态可能不会
    立刻反映出来，而 nvidia-smi 每次都会直接向驱动查询，能更快发现
    "Failed to initialize NVML" / "Unable to determine the device handle" 等故障。
    """
    nvidia_smi = shutil.which("nvidia-smi")
    if nvidia_smi is None:
        return False, "nvidia-smi executable not found"

    try:
        result = subprocess.run(
            [nvidia_smi, "--query-gpu=index,name", "--format=csv,noheader"],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired:
        return False, f"nvidia-smi did not respond within {timeout}s"
    except OSError as exc:
        return False, f"Failed to execute nvidia-smi: {exc}"

    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        return False, f"nvidia-smi exited with code {result.returncode}: {detail}"

    if not result.stdout.strip():
        return False, "nvidia-smi returned no GPU information"

    return True, None


if __name__ == "__main__":
    print("Is Mac Environment:", is_mac_environment())
    print("Is Apple Silicon CPU:", is_apple_silicon_cpu())
    print("Is Mac OS Version Supported (>=13.5):", is_mac_os_version_supported())