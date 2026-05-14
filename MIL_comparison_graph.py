import matplotlib.pyplot as plt
import pandas as pd
import re

# List of your uploaded files and their corresponding labels for the paper
filenames = [
    "metrics_5001.txt",
    "metrics_5001_with_Quantization.txt",
    "metrics_5000_Combo.txt",
    "metrics_5000_Coordinated_Attack.txt"
]
labels = ["Base", "Quantized", "Combo", "Attack"]


def parse_metrics(filename):
    with open(filename, 'r') as f:
        content = f.read()

    data = {}

    # 1. Parse CPU usage
    cpu = re.search(r'process_cpu_seconds_total ([\d.]+)', content)
    data['cpu_seconds'] = float(cpu.group(1)) if cpu else None

    # 2. Parse Resident Memory (RSS) - converting bytes to MB
    rss = re.search(r'process_resident_memory_bytes ([\d.e+]+)', content)
    data['rss_mb'] = float(rss.group(1)) / (1024 * 1024) if rss else None

    # 3. Parse Trust Threshold
    threshold = re.search(r'mil_(?:system|dynamic)_threshold ([\d.]+)', content)
    data['threshold'] = float(threshold.group(1)) if threshold else None

    # 4. Parse Routing Latency (Calculating average from Sum and Count)
    r_sum = re.search(r'mil_routing_latency_(?:milliseconds|ms)_sum ([\d.]+)', content)
    r_count = re.search(r'mil_routing_latency_(?:milliseconds|ms)_count ([\d.]+)', content)
    if r_sum and r_count:
        data['avg_routing_latency_ms'] = float(r_sum.group(1)) / float(r_count.group(1))
    else:
        data['avg_routing_latency_ms'] = 0

    # 5. Parse Inference Latency (Standard FP32)
    inf_fp32_sum = re.search(r'mil_inference_latency_ms_sum\{model_type="Standard_FP32"\} ([\d.]+)', content)
    inf_fp32_count = re.search(r'mil_inference_latency_ms_count\{model_type="Standard_FP32"\} ([\d.]+)', content)
    if inf_fp32_sum and inf_fp32_count:
        data['avg_inf_fp32_ms'] = float(inf_fp32_sum.group(1)) / float(inf_fp32_count.group(1))
        data['count_fp32'] = float(inf_fp32_count.group(1))
    else:
        data['avg_inf_fp32_ms'] = 0
        data['count_fp32'] = 0

    # 6. Parse Inference Latency (TFLite INT8)
    inf_int8_sum = re.search(r'mil_inference_latency_ms_sum\{model_type="TFLite_INT8_Quantized"\} ([\d.]+)', content)
    inf_int8_count = re.search(r'mil_inference_latency_ms_count\{model_type="TFLite_INT8_Quantized"\} ([\d.]+)',
                               content)
    if inf_int8_sum and inf_int8_count:
        data['avg_inf_int8_ms'] = float(inf_int8_sum.group(1)) / float(inf_int8_count.group(1))
        data['count_int8'] = float(inf_int8_count.group(1))
    else:
        data['avg_inf_int8_ms'] = 0
        data['count_int8'] = 0

    return data


# Process all files into a DataFrame
results = [parse_metrics(fname) for fname in filenames]
df = pd.DataFrame(results, index=labels)

# --- Graph 1: Resource Usage (CPU and RAM) ---
fig, ax1 = plt.subplots(figsize=(10, 6))
ax2 = ax1.twinx()
df['cpu_seconds'].plot(kind='bar', ax=ax1, color='skyblue', position=1, width=0.4, label='CPU Time (s)')
df['rss_mb'].plot(kind='bar', ax=ax2, color='salmon', position=0, width=0.4, label='Resident Memory (MB)')
ax1.set_ylabel('CPU Time (Seconds)')
ax2.set_ylabel('Resident Memory (MB)')
ax1.set_title('Resource Consumption across Scenarios')
ax1.legend(loc='upper left')
ax2.legend(loc='upper right')
plt.tight_layout()
plt.savefig('resource_usage.png')

# --- Graph 2: Inference Latency Comparison ---
inf_df = df[['avg_inf_fp32_ms', 'avg_inf_int8_ms']]
inf_df = inf_df[inf_df.sum(axis=1) > 0]  # Filter out scenarios with no inference data
inf_df.plot(kind='bar', figsize=(10, 6), color=['#2ca02c', '#d62728'])
plt.title('Average Inference Latency: Standard FP32 vs. TFLite INT8')
plt.ylabel('Latency (ms)')
plt.legend(['Standard FP32', 'TFLite INT8'])
plt.tight_layout()
plt.savefig('inference_latency.png')

# --- Graph 3: Security vs Routing Overhead ---
fig, ax1 = plt.subplots(figsize=(10, 6))
ax2 = ax1.twinx()
df['threshold'].plot(kind='line', marker='o', ax=ax1, color='purple', label='Trust Threshold')
df['avg_routing_latency_ms'].plot(kind='line', marker='s', ax=ax2, color='orange', label='Avg Routing Latency (ms)')
ax1.set_ylabel('Security Threshold')
ax2.set_ylabel('Latency (ms)')
ax1.set_title('Security Adaptation vs. Routing Overhead')
ax1.legend(loc='upper left')
ax2.legend(loc='upper right')
plt.grid(True, linestyle='--', alpha=0.7)
plt.tight_layout()
plt.savefig('security_vs_latency.png')

# --- Graph 4: Model Selection Strategy (%) ---
dist_df = df[['count_fp32', 'count_int8']].loc[['Quantized', 'Combo', 'Attack']]
dist_df_pct = dist_df.div(dist_df.sum(axis=1), axis=0) * 100
dist_df_pct.plot(kind='bar', stacked=True, figsize=(10, 6), color=['#1f77b4', '#ff7f0e'])
plt.title('Model Selection Distribution Strategy (%)')
plt.ylabel('Percentage of Messages')
plt.legend(['Standard FP32', 'TFLite INT8'], bbox_to_anchor=(1.05, 1), loc='upper left')
plt.tight_layout()
plt.savefig('model_distribution.png')