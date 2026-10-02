FROM python:3.11-slim

WORKDIR /app

# Install system dependencies
# Install system dependencies for xmlsec and lxml
RUN apt-get update && \
    apt-get install -y \
    curl \
    libxml2-dev \
    libxslt1-dev \
    libxmlsec1-dev \
    libxmlsec1-openssl \
    pkg-config \
    gcc \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir lxml==4.9.4 setuptools_scm
COPY requirements.txt .
RUN pip install --no-cache-dir --no-build-isolation -r requirements.txt

# Copy application
COPY app.py .

# Expose port
EXPOSE 8081

# Run the application
CMD ["python", "app.py"]
