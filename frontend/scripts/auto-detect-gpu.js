#!/usr/bin/env node

const { execSync } = require('child_process');
const os = require('os');

function commandExists(cmd) {
  try {
    execSync(`${os.platform() === 'win32' ? 'where' : 'which'} ${cmd}`, { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
}

function detectGPU() {
  const platform = os.platform();

  if (platform === 'darwin') {
    const arch = os.arch();
    if (arch === 'arm64') {
      console.log('🍎 Apple Silicon detected - using Metal + CoreML');
      return 'coreml';
    } else {
      console.log('🍎 macOS Intel detected - using Metal');
      return 'metal';
    }
  }

  if (platform === 'win32' || platform === 'linux') {
    if (commandExists('nvidia-smi')) {
      const cudaPath = process.env.CUDA_PATH;
      if (cudaPath || commandExists('nvcc')) {
        console.log('🟢 NVIDIA GPU detected with CUDA - using CUDA acceleration');
        return 'cuda';
      } else {
        console.log('⚠️  NVIDIA GPU detected but CUDA not installed - falling back to CPU');
        return null;
      }
    }

    if (platform === 'linux' && commandExists('rocm-smi')) {
      const rocmPath = process.env.ROCM_PATH;
      if (rocmPath || commandExists('hipcc')) {
        console.log('🔴 AMD GPU detected with ROCm - using HIPBlas acceleration');
        return 'hipblas';
      } else {
        console.log('⚠️  AMD GPU detected but ROCm not installed - falling back to CPU');
        return null;
      }
    }

    if (commandExists('vulkaninfo') || (platform === 'win32' && require('fs').existsSync('C:\\VulkanSDK'))) {
      const vulkanSdk = process.env.VULKAN_SDK;
      const blasInclude = process.env.BLAS_INCLUDE_DIRS;

      if (vulkanSdk && blasInclude) {
        console.log('🔵 Vulkan detected with all dependencies - using Vulkan acceleration');
        return 'vulkan';
      } else {
        console.log('⚠️  Vulkan detected but missing dependencies - falling back to CPU');
        if (!vulkanSdk) console.log('   Missing: VULKAN_SDK environment variable');
        if (!blasInclude) console.log('   Missing: BLAS_INCLUDE_DIRS environment variable');
        return null;
      }
    }

    const blasInclude = process.env.BLAS_INCLUDE_DIRS;
    if (blasInclude) {
      console.log('📊 OpenBLAS detected - using CPU with BLAS optimization');
      return 'openblas';
    }
  }

  console.log('💻 No GPU acceleration available - using CPU-only mode');
  return null;
}

const originalLog = console.log;
console.log = (...args) => {
  process.stderr.write(args.join(' ') + '\n');
};

const feature = detectGPU();

console.log = originalLog;

if (feature) {
  process.stdout.write(feature);
}
