# elm-format only works with buster variants of the NodeJS container image.
# See https://github.com/avh4/elm-format/issues/709 for more details.
FROM node:14-buster

ENV NPM_CONFIG_PREFIX=/home/node/.npm-global
ENV PATH=$PATH:/home/node/.npm-global/bin

RUN mkdir -p $NPM_CONFIG_PREFIX; yarn global add \
  elm@0.19.1 \
  elm-format@0.8.4 \
  elm-test@0.19.1-revision6 \
  uglify-js@3.4.7
