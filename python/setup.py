"""Setup for the UIR Python model."""

from setuptools import setup

setup(
    name='uir',
    version='1.0.0',
    description='Universal Intermediate Representation (UIR) model for Python',
    url='https://github.com/flanksource/uir',
    package_dir={'uir': '.'},
    packages=['uir'],
    python_requires='>=3.8',
)
