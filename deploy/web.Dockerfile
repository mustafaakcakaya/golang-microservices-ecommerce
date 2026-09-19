# The storefront is built once and then served as static files: there is no
# server-side rendering, so nothing but nginx needs to run.
ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-alpine AS build
WORKDIR /src

# The lockfile first, so a source change does not reinstall the dependencies.
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
# Type checking is part of the build: a type error should fail here rather
# than reach a browser.
RUN npm run build

FROM nginx:1.27-alpine AS final

COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html

EXPOSE 8080
